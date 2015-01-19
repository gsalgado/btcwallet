/*
 * Copyright (c) 2015 Conformal Systems LLC <info@conformal.com>
 *
 * Permission to use, copy, modify, and distribute this software for any
 * purpose with or without fee is hereby granted, provided that the above
 * copyright notice and this permission notice appear in all copies.
 *
 * THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
 * WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
 * MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
 * ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
 * WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
 * ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
 * OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.
 */

package votingpool

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/btcsuite/btcscript"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/txstore"
	"github.com/btcsuite/btcwallet/waddrmgr"
	"github.com/btcsuite/btcwire"
	"github.com/btcsuite/fastsha256"
)

/*  ==== What needs to be stored in the DB, and other notes ====
(This is just a collection of notes about things we still need to do here)

== To be stored in the DB ==

Signature lists

All parameters of a startwithdrawal, to be able to return an error if we get two of them
with the same roundID but anything else different.

The whole WithdrawalStatus so that we can deal with multiple identical startwithdrawal
requests. We need to do that because the transactions created as part of a startwithdrawal
will mark outputs as spent and if we get a second identical startwithdrawal request we
won't be able construct the same transactions as we did in the first request.

== Other notes ==

Using separate DB Buckets for the transactions and the withdrawal registry (siglists, etc)
may be problematic.  We'll need to make sure both the transactions and the withdrawal
details are persisted atomically.

Since we're marking outputs as spent when we use them in transactions constructed in
startwithdrawal, we'll need a janitor process that eventually releases those if the
transactions are never confirmed.

*/

// Maximum tx size (in bytes). This should be the same as bitcoind's
// MAX_STANDARD_TX_SIZE.
const txMaxSize = 100000

// feeIncrement is the minimum transation fee (0.00001 BTC, measured in satoshis)
// added to transactions requiring a fee.
const feeIncrement = 1e3

const outputRequestStatusSuccess = "success"
const outputRequestStatusPartial = "partial-"

type WithdrawalStatus struct {
	nextInputStart  WithdrawalAddress
	nextChangeStart ChangeAddress
	fees            btcutil.Amount
	outputs         map[string]*WithdrawalOutput
}

func (s *WithdrawalStatus) Outputs() map[string]*WithdrawalOutput {
	return s.outputs
}

// byAmount defines the methods needed to satisify sort.Interface to
// sort a slice of OutputRequests by their amount.
type byAmount []OutputRequest

func (u byAmount) Len() int           { return len(u) }
func (u byAmount) Less(i, j int) bool { return u[i].amount < u[j].amount }
func (u byAmount) Swap(i, j int)      { u[i], u[j] = u[j], u[i] }

// byOutBailmentID defines the methods needed to satisify sort.Interface to sort
// a slice of OutputRequests by their outBailmentIDHash.
type byOutBailmentID []OutputRequest

func (s byOutBailmentID) Len() int      { return len(s) }
func (s byOutBailmentID) Swap(i, j int) { s[i], s[j] = s[j], s[i] }
func (s byOutBailmentID) Less(i, j int) bool {
	return bytes.Compare(s[i].outBailmentIDHash(), s[j].outBailmentIDHash()) < 0
}

// OutputRequest represents one of the outputs (address/amount) requested by a
// withdrawal, and includes information about the user's outbailment request.
type OutputRequest struct {
	address  btcutil.Address
	amount   btcutil.Amount
	pkScript []byte

	// The notary server that received the outbailment request.
	server string

	// The server-specific transaction number for the outbailment request.
	transaction uint32

	// cachedHash is used to cache the hash of the outBailmentID so it
	// only has to be calculated once.
	cachedHash []byte
}

// String makes OutputRequest satisfy the Stringer interface.
func (r OutputRequest) String() string {
	return fmt.Sprintf("OutputRequest %s to send %v to %s", r.outBailmentID(), r.amount, r.address)
}

func (r OutputRequest) outBailmentID() string {
	return fmt.Sprintf("%s:%d", r.server, r.transaction)
}

// outBailmentIDHash returns a byte slice which is used when sorting
// OutputRequests.
func (r OutputRequest) outBailmentIDHash() []byte {
	if r.cachedHash != nil {
		return r.cachedHash
	}
	str := fmt.Sprintf("%s%d", r.server, r.transaction)
	hasher := fastsha256.New()
	// hasher.Write() always returns nil as the error, so it's safe to ignore it here.
	_, _ = hasher.Write([]byte(str))
	id := hasher.Sum(nil)
	r.cachedHash = id
	return id
}

// WithdrawalOutput represents a possibly fulfilled OutputRequest.
type WithdrawalOutput struct {
	request OutputRequest
	status  string
	// The outpoints that fulfill the OutputRequest. There will be more than one in case we
	// need to split the request across multiple transactions.
	outpoints []OutBailmentOutpoint
}

func (o *WithdrawalOutput) String() string {
	return fmt.Sprintf("WithdrawalOutput for %s", o.request)
}

func (o *WithdrawalOutput) addOutpoint(outpoint OutBailmentOutpoint) {
	o.outpoints = append(o.outpoints, outpoint)
}

func (o *WithdrawalOutput) Status() string {
	return o.status
}

func (o *WithdrawalOutput) Address() string {
	return o.request.address.String()
}

func (o *WithdrawalOutput) Outpoints() []OutBailmentOutpoint {
	return o.outpoints
}

// OutBailmentOutpoint represents one of the outpoints created to fulfil an OutputRequest.
type OutBailmentOutpoint struct {
	ntxid  string
	index  uint32
	amount btcutil.Amount
}

func (o OutBailmentOutpoint) Amount() btcutil.Amount {
	return o.amount
}

// TxSigs is list of raw signatures (one for every pubkey in the multi-sig
// script) for a given transaction input. They should match the order of pubkeys
// in the script and an empty RawSig should be used when the private key for a
// pubkey is not known.
type TxSigs [][]RawSig

type RawSig []byte

// withdrawal holds all the state needed for Pool.Withdrawal() to do its job.
type withdrawal struct {
	roundID         uint32
	status          *WithdrawalStatus
	changeStart     *ChangeAddress
	transactions    []*withdrawalTx
	pendingRequests []OutputRequest
	eligibleInputs  []CreditInterface
	current         *withdrawalTx
	// newWithdrawalTx is a member of the structure so it can be replaced for
	// testing purposes.
	newWithdrawalTx func() *withdrawalTx
}

// withdrawalTxOut wraps an OutputRequest and provides a separate amount field.
// It is necessary because some requests may be partially fulfilled or split
// across transactions.
type withdrawalTxOut struct {
	// Notice that in the case of a split output, the OutputRequest here will
	// be a copy of the original one with the amount being the remainder of the
	// originally requested amount minus the amounts fulfilled by other
	// withdrawalTxOut. The original OutputRequest, if needed, can be obtained
	// from WithdrawalStatus.outputs.
	request OutputRequest
	amount  btcutil.Amount
}

// String makes withdrawalTxOut satisfy the Stringer interface.
func (o *withdrawalTxOut) String() string {
	return fmt.Sprintf("withdrawalTxOut fulfilling %v of %s", o.amount, o.request)
}

func (o *withdrawalTxOut) pkScript() []byte {
	return o.request.pkScript
}

// withdrawalTx represents a transaction constructed by the withdrawal process.
type withdrawalTx struct {
	inputs  []CreditInterface
	outputs []*withdrawalTxOut
	fee     btcutil.Amount

	// calculateFee calculates the expected network fees for this transaction.
	// We use a func() field instead of a method so that it can be replaced in
	// tests.
	calculateFee func() btcutil.Amount

	// isTooBig is a member of the structure so it can be replaced for testing
	// purposes.
	isTooBig func() bool

	// calculateSize is a member of the structure so it can be replaced for testing
	// purposes.
	calculateSize func() int

	// changeOutput holds information about the change for this transaction.
	changeOutput *btcwire.TxOut
}

func newWithdrawalTx() *withdrawalTx {
	tx := &withdrawalTx{}
	tx.calculateFee = func() btcutil.Amount {
		return btcutil.Amount(1+tx.calculateSize()/1000) * feeIncrement
	}
	tx.isTooBig = func() bool {
		// In bitcoind a tx is considered standard only if smaller than
		// MAX_STANDARD_TX_SIZE; that's why we consider anything >= txMaxSize to
		// be too big.
		return tx.calculateSize() >= txMaxSize
	}
	tx.calculateSize = func() int {
		return calculateSize(tx)
	}
	return tx
}

// inputTotal returns the sum amount of all inputs in this tx.
func (tx *withdrawalTx) inputTotal() (total btcutil.Amount) {
	for _, input := range tx.inputs {
		total += input.Amount()
	}
	return total
}

// outputTotal returns the sum amount of all outputs in this tx. It does not
// include the amount for the change output, in case the tx has one.
func (tx *withdrawalTx) outputTotal() (total btcutil.Amount) {
	for _, output := range tx.outputs {
		total += output.amount
	}
	return total
}

// hasChange returns true if this transaction has a change output.
func (tx *withdrawalTx) hasChange() bool {
	return tx.changeOutput != nil
}

// toMsgTx generates a btcwire.MsgTx with this tx's inputs and outputs.
func (tx *withdrawalTx) toMsgTx() *btcwire.MsgTx {
	msgtx := btcwire.NewMsgTx()
	for _, o := range tx.outputs {
		msgtx.AddTxOut(btcwire.NewTxOut(int64(o.amount), o.pkScript()))
	}

	if tx.hasChange() {
		msgtx.AddTxOut(tx.changeOutput)
	}

	for _, i := range tx.inputs {
		msgtx.AddTxIn(btcwire.NewTxIn(i.OutPoint(), nil))
	}
	return msgtx
}

// addOutput adds a new output to this transaction.
func (tx *withdrawalTx) addOutput(request OutputRequest) {
	log.Debugf("Added tx output sending %s to %s", request.amount, request.address)
	tx.outputs = append(tx.outputs, &withdrawalTxOut{request: request, amount: request.amount})
}

// removeOutput removes the last added output and returns it.
func (tx *withdrawalTx) removeOutput() *withdrawalTxOut {
	removed := tx.outputs[len(tx.outputs)-1]
	tx.outputs = tx.outputs[:len(tx.outputs)-1]
	log.Debugf("Removed tx output sending %s to %s", removed.amount, removed.request.address)
	return removed
}

// addInput adds a new input to this transaction.
func (tx *withdrawalTx) addInput(input CreditInterface) {
	log.Debugf("Added tx input with amount %v", input.Amount())
	tx.inputs = append(tx.inputs, input)
}

// removeInput removes the last added input and returns it.
func (tx *withdrawalTx) removeInput() CreditInterface {
	removed := tx.inputs[len(tx.inputs)-1]
	tx.inputs = tx.inputs[:len(tx.inputs)-1]
	log.Debugf("Removed tx input with amount %v", removed.Amount())
	return removed
}

// addChange adds a change output if there are any satoshis left after paying
// all the outputs and network fees. It returns true if a change output was
// added.
//
// This method must be called only once, and no extra inputs/outputs should be
// added after it's called. Also, callsites must make sure adding a change
// output won't cause the tx to exceed the size limit.
func (tx *withdrawalTx) addChange(pkScript []byte) bool {
	tx.fee = tx.calculateFee()
	change := tx.inputTotal() - tx.outputTotal() - tx.fee
	log.Debugf("addChange: input total %v, output total %v, fee %v", tx.inputTotal(),
		tx.outputTotal(), tx.fee)
	if change > 0 {
		tx.changeOutput = btcwire.NewTxOut(int64(change), pkScript)
		log.Debugf("Added change output with amount %v", change)
	}
	return tx.hasChange()
}

// rollBackLastOutput will roll back the last added output and possibly remove
// inputs that are no longer needed to cover the remaining outputs. The method
// returns the removed output and the removed inputs, in the reverse order they
// were added, if any.
//
// The tx needs to have two or more outputs. The case with only one output must
// be handled separately (by the split output procedure).
func (tx *withdrawalTx) rollBackLastOutput() ([]CreditInterface, *withdrawalTxOut, error) {
	// Check precondition: At least two outputs are required in the transaction.
	if len(tx.outputs) < 2 {
		str := fmt.Sprintf("at least two outputs expected; got %d", len(tx.outputs))
		return nil, nil, newError(ErrPreconditionNotMet, str, nil)
	}

	removedOutput := tx.removeOutput()

	var removedInputs []CreditInterface
	// Continue until sum(in) < sum(out) + fee
	for tx.inputTotal() >= tx.outputTotal()+tx.calculateFee() {
		removedInputs = append(removedInputs, tx.removeInput())
	}

	// Re-add the last item from removedInputs, which is the last popped input.
	tx.addInput(removedInputs[len(removedInputs)-1])
	removedInputs = removedInputs[:len(removedInputs)-1]
	return removedInputs, removedOutput, nil
}

func newWithdrawal(roundID uint32, requests []OutputRequest, inputs []CreditInterface,
	changeStart *ChangeAddress) *withdrawal {
	outputs := make(map[string]*WithdrawalOutput, len(requests))
	for _, request := range requests {
		outputs[request.outBailmentID()] = &WithdrawalOutput{request: request}
	}
	return &withdrawal{
		roundID:         roundID,
		current:         newWithdrawalTx(),
		pendingRequests: requests,
		eligibleInputs:  inputs,
		status:          &WithdrawalStatus{outputs: outputs},
		changeStart:     changeStart,
		newWithdrawalTx: newWithdrawalTx,
	}
}

func (vp *Pool) Withdrawal(
	roundID uint32,
	requests []OutputRequest,
	inputs []CreditInterface,
	changeStart *ChangeAddress,
	txStore *txstore.Store,
) (*WithdrawalStatus, map[string]TxSigs, error) {
	w := newWithdrawal(roundID, requests, inputs, changeStart)
	if err := w.fulfillRequests(); err != nil {
		return nil, nil, err
	}
	sigs, err := getRawSigs(w.transactions)
	if err != nil {
		return nil, nil, err
	}

	if err := storeTransactions(txStore, w.transactions); err != nil {
		return nil, nil, err
	}

	return w.status, sigs, nil
}

// storeTransactions generates the msgtx transaction from the given transactions
// and adds them to the txStore and writes it to disk. The credits used in each
// transaction are removed from the store's unspent list, and if a transaction
// includes a change output, it is added to the store as a credit.
//
// TODO: Wrap the errors we catch here in a custom votingpool.Error before
// returning.
func storeTransactions(txStore *txstore.Store, transactions []*withdrawalTx) error {
	for _, tx := range transactions {
		msgtx := tx.toMsgTx()
		txr, err := txStore.InsertTx(btcutil.NewTx(msgtx), nil)
		if err != nil {
			return err
		}
		if _, err = txr.AddDebits(); err != nil {
			return err
		}
		if tx.hasChange() {
			idx, err := getTxOutIndex(tx.changeOutput, msgtx)
			if err != nil {
				return err
			}
			if _, err = txr.AddCredit(idx, true); err != nil {
				return err
			}
		}
	}
	txStore.MarkDirty()
	if err := txStore.WriteIfDirty(); err != nil {
		return err
	}
	return nil
}

// getTxOutIndex returns the index of the TxOut in the MsgTx.
func getTxOutIndex(txout *btcwire.TxOut, msgtx *btcwire.MsgTx) (uint32, error) {
	for i, o := range msgtx.TxOut {
		if o == txout {
			return uint32(i), nil
		}
	}
	return 0, newError(ErrTxOutNotFound, "", nil)
}

// popRequest removes and returns the first request from the stack of pending
// requests.
func (w *withdrawal) popRequest() OutputRequest {
	request := w.pendingRequests[0]
	w.pendingRequests = w.pendingRequests[1:]
	return request
}

// pushRequest adds a new request to the top of the stack of pending requests.
func (w *withdrawal) pushRequest(request OutputRequest) {
	w.pendingRequests = append([]OutputRequest{request}, w.pendingRequests...)
}

// popInput removes and returns the first input from the stack of eligible
// inputs.
func (w *withdrawal) popInput() CreditInterface {
	input := w.eligibleInputs[0]
	w.eligibleInputs = w.eligibleInputs[1:]
	return input
}

// pushInput adds a new input to the top of the stack of eligible inputs.
func (w *withdrawal) pushInput(input CreditInterface) {
	w.eligibleInputs = append([]CreditInterface{input}, w.eligibleInputs...)
}

// If this returns it means we have added an output and the necessary inputs to fulfil that
// output plus the required fees. It also means the tx won't reach the size limit even
// after we add a change output and sign all inputs.
func (w *withdrawal) fulfillNextRequest() error {
	request := w.popRequest()
	output := w.status.outputs[request.outBailmentID()]
	// We start with an output status of success and if anything goes wrong it
	// will be changed.
	output.status = outputRequestStatusSuccess
	w.current.addOutput(request)

	if w.current.isTooBig() {
		return w.handleOversizeTx()
	}

	fee := w.current.calculateFee()
	for w.current.inputTotal() < w.current.outputTotal()+fee {
		if len(w.eligibleInputs) == 0 {
			log.Debug("Splitting last output because we don't have enough inputs")
			if err := w.splitLastOutput(); err != nil {
				return err
			}
			break
		}
		w.current.addInput(w.popInput())
		fee = w.current.calculateFee()

		if w.current.isTooBig() {
			return w.handleOversizeTx()
		}
	}
	return nil
}

// handleOversizeTx handles the case when a transaction has become too
// big by either rolling back an output or splitting it.
func (w *withdrawal) handleOversizeTx() error {
	if len(w.current.outputs) > 1 {
		log.Debug("Rolling back last output because tx got too big")
		inputs, output, err := w.current.rollBackLastOutput()
		if err != nil {
			return newError(ErrWithdrawalProcessing, "failed to rollback last output", err)
		}
		for _, input := range inputs {
			w.pushInput(input)
		}
		w.pushRequest(output.request)
	} else if len(w.current.outputs) == 1 {
		log.Debug("Splitting last output because tx got too big...")
		w.pushInput(w.current.removeInput())
		if err := w.splitLastOutput(); err != nil {
			return err
		}
	} else {
		return newError(
			ErrPreconditionNotMet, "Oversize tx must have at least one output", nil)
	}
	return w.finalizeCurrentTx()
}

// finalizeCurrentTx finalizes the transaction in w.current, moves it to the
// list of finalized transactions and replaces w.current with a new empty
// transaction.
func (w *withdrawal) finalizeCurrentTx() error {
	log.Debug("Finalizing current transaction")
	tx := w.current
	if len(tx.outputs) == 0 {
		log.Debug("Current transaction has no outputs, doing nothing")
		return nil
	}

	pkScript, err := btcscript.PayToAddrScript(w.changeStart.addr)
	if err != nil {
		return newError(
			ErrWithdrawalProcessing, "failed to generate pkScript for change address", err)
	}
	if tx.addChange(pkScript) {
		var err error
		w.changeStart, err = w.changeStart.Next()
		if err != nil {
			return newError(
				ErrWithdrawalProcessing, "failed to get next change address", err)
		}
	}

	ntxid := Ntxid(tx.toMsgTx())
	for i, txOut := range tx.outputs {
		outputStatus := w.status.outputs[txOut.request.outBailmentID()]
		outputStatus.addOutpoint(
			OutBailmentOutpoint{ntxid: ntxid, index: uint32(i), amount: txOut.amount})
	}

	// Check that WithdrawalOutput entries with status==success have the sum of
	// their outpoint amounts matching the requested amount.
	for _, txOut := range tx.outputs {
		// Look up the original request we received because txOut.request may
		// represent a split request and thus have a different amount from the
		// original one.
		outputStatus := w.status.outputs[txOut.request.outBailmentID()]
		origRequest := outputStatus.request
		amtFulfilled := btcutil.Amount(0)
		for _, outpoint := range outputStatus.outpoints {
			amtFulfilled += outpoint.amount
		}
		if outputStatus.status == outputRequestStatusSuccess && amtFulfilled != origRequest.amount {
			msg := fmt.Sprintf(
				"%s was not completely fulfilled; only %v fulfilled", origRequest, amtFulfilled)
			return newError(ErrWithdrawalProcessing, msg, nil)
		}
	}

	w.transactions = append(w.transactions, tx)
	w.current = w.newWithdrawalTx()
	return nil
}

// maybeDropRequests will check the total amount we have in eligible inputs and drop
// requested outputs (in descending amount order) if we don't have enough to
// fulfill them all. For every dropped output request we update its entry in
// w.status.outputs with the status string set to outputRequestStatusPartial.
func (w *withdrawal) maybeDropRequests() {
	inputAmount := btcutil.Amount(0)
	for _, input := range w.eligibleInputs {
		inputAmount += input.Amount()
	}
	outputAmount := btcutil.Amount(0)
	for _, request := range w.pendingRequests {
		outputAmount += request.amount
	}
	sort.Sort(sort.Reverse(byAmount(w.pendingRequests)))
	for inputAmount < outputAmount {
		request := w.popRequest()
		log.Infof("Not fulfilling request to send %v to %v; not enough credits.",
			request.amount, request.address)
		outputAmount -= request.amount
		w.status.outputs[request.outBailmentID()].status = outputRequestStatusPartial
	}
}

func (w *withdrawal) fulfillRequests() error {
	w.maybeDropRequests()
	if len(w.pendingRequests) == 0 {
		return nil
	}

	// Sort outputs by outBailmentID (hash(server ID, tx #))
	sort.Sort(byOutBailmentID(w.pendingRequests))

	for len(w.pendingRequests) > 0 {
		if err := w.fulfillNextRequest(); err != nil {
			return err
		}
		tx := w.current
		if len(w.eligibleInputs) == 0 && tx.inputTotal() <= tx.outputTotal()+tx.calculateFee() {
			// We don't have more eligible inputs and all the inputs in the
			// current tx have been spent.
			break
		}
	}

	if err := w.finalizeCurrentTx(); err != nil {
		return err
	}

	for _, tx := range w.transactions {
		w.updateStatusFor(tx)
		w.status.fees += tx.fee
	}
	return nil
}

func (w *withdrawal) splitLastOutput() error {
	if len(w.current.outputs) == 0 {
		return newError(ErrPreconditionNotMet,
			"splitLastOutput requires current tx to have at least 1 output", nil)
	}

	tx := w.current
	output := tx.outputs[len(tx.outputs)-1]
	log.Debugf("Splitting tx output for %s", output.request)
	origAmount := output.amount
	spentAmount := tx.outputTotal() + tx.calculateFee() - output.amount
	// This is how much we have left after satisfying all outputs except the last
	// one. IOW, all we have left for the last output, so we set that as the
	// amount of the tx's last output.
	unspentAmount := tx.inputTotal() - spentAmount
	output.amount = unspentAmount
	log.Debugf("Updated output amount to %v", output.amount)

	// Create a new OutputRequest with the amount being the difference between
	// the original amount and what was left in the tx output above.
	request := output.request
	newRequest := OutputRequest{
		server:      request.server,
		transaction: request.transaction,
		address:     request.address,
		pkScript:    request.pkScript,
		amount:      origAmount - output.amount}
	w.pushRequest(newRequest)
	log.Debugf("Created a new pending output request with amount %v", newRequest.amount)

	w.status.outputs[request.outBailmentID()].status = outputRequestStatusPartial
	return nil
}

func (w *withdrawal) updateStatusFor(tx *withdrawalTx) {
	// TODO
}

// Ntxid returns a unique ID for the given transaction.
func Ntxid(tx *btcwire.MsgTx) string {
	// According to https://blockchain.info/q, the ntxid is the "hash of the serialized
	// transaction with its input scripts blank". But since we store the tx with
	// blank SignatureScripts anyway, we can use tx.TxSha() as the ntxid, which makes
	// our lives easier as that is what the txstore uses to lookup transactions.
	// Ignore the error as TxSha() can't fail.
	sha, _ := tx.TxSha()
	return sha.String()
}

// getRawSigs iterates over the inputs of each transaction given, constructing the
// raw signatures for them using the private keys available to us.
// creditsUsed must have one entry for every transaction, with the transaction's ntxid
// as the key and a slice of credits spent by that transaction as the value.
// It returns a map of ntxids to signature lists.
func getRawSigs(transactions []*withdrawalTx) (map[string]TxSigs, error) {
	sigs := make(map[string]TxSigs)
	for _, tx := range transactions {
		txSigs := make(TxSigs, len(tx.inputs))
		msgtx := tx.toMsgTx()
		ntxid := Ntxid(msgtx)
		for inputIdx, input := range tx.inputs {
			creditAddr := input.Address()
			redeemScript := creditAddr.RedeemScript()
			series := creditAddr.Series()
			// The order of the raw signatures in the signature script must match the
			// order of the public keys in the redeem script, so we sort the public keys
			// here using the same API used to sort them in the redeem script and use
			// series.getPrivKeyFor() to lookup the corresponding private keys.
			pubKeys, err := branchOrder(series.publicKeys, creditAddr.Branch())
			if err != nil {
				return nil, err
			}
			txInSigs := make([]RawSig, len(pubKeys))
			for i, pubKey := range pubKeys {
				var sig RawSig
				privKey, err := series.getPrivKeyFor(pubKey)
				if err != nil {
					return nil, err
				}
				if privKey != nil {
					childKey, err := privKey.Child(uint32(creditAddr.Index()))
					if err != nil {
						return nil, newError(ErrKeyChain, "failed to derive private key", err)
					}
					ecPrivKey, err := childKey.ECPrivKey()
					if err != nil {
						return nil, newError(ErrKeyChain, "failed to obtain ECPrivKey", err)
					}
					log.Debugf("Signing input %d of tx %s with privkey of %s",
						inputIdx, ntxid, pubKey.String())
					sig, err = btcscript.RawTxInSignature(
						msgtx, inputIdx, redeemScript, btcscript.SigHashAll, ecPrivKey)
					if err != nil {
						return nil, newError(
							ErrRawSigning, "failed to generate raw signature", err)
					}
				} else {
					log.Debugf(
						"Not signing input %d of %s because private key for %s is "+
							"not available: %v", inputIdx, ntxid, pubKey.String(), err)
					sig = []byte{}
				}
				txInSigs[i] = sig
			}
			txSigs[inputIdx] = txInSigs
		}
		sigs[ntxid] = txSigs
	}
	return sigs, nil
}

// SignTx signs every input of the given MsgTx by looking up (on the addr
// manager) the redeem script for each of them and constructing the signature
// script using that and the given raw signatures.
// This function must be called with the manager unlocked.
func SignTx(msgtx *btcwire.MsgTx, sigs TxSigs, mgr *waddrmgr.Manager, store *txstore.Store) error {
	for i, txIn := range msgtx.TxIn {
		txOut, err := store.UnconfirmedSpent(txIn.PreviousOutPoint)
		if err != nil {
			errStr := fmt.Sprintf("unable to find previous outpoint of tx input #%d", i)
			return newError(ErrTxSigning, errStr, err)
		}
		if err = signMultiSigUTXO(mgr, msgtx, i, txOut.PkScript, sigs[i]); err != nil {
			return err
		}
	}
	return nil
}

// getRedeemScript returns the redeem script for the given P2SH address. It must
// be called with the manager unlocked.
func getRedeemScript(mgr *waddrmgr.Manager, addr *btcutil.AddressScriptHash) ([]byte, error) {
	address, err := mgr.Address(addr)
	if err != nil {
		return nil, err
	}
	return address.(waddrmgr.ManagedScriptAddress).Script()
}

// signMultiSigUTXO signs the P2SH UTXO with the given index by constructing a
// script containing all given signatures plus the redeem (multi-sig) script. The
// redeem script is obtained by looking up the address of the given P2SH pkScript
// on the address manager.
// The order of the signatures must match that of the public keys in the multi-sig
// script as OP_CHECKMULTISIG expects that.
// This function must be called with the manager unlocked.
func signMultiSigUTXO(mgr *waddrmgr.Manager, tx *btcwire.MsgTx, idx int, pkScript []byte, sigs []RawSig) error {
	class, addresses, _, err := btcscript.ExtractPkScriptAddrs(pkScript, mgr.Net())
	if err != nil {
		return newError(ErrTxSigning, "unparseable pkScript", err)
	}
	if class != btcscript.ScriptHashTy {
		return newError(ErrTxSigning, fmt.Sprintf("pkScript is not P2SH: %s", class), nil)
	}
	redeemScript, err := getRedeemScript(mgr, addresses[0].(*btcutil.AddressScriptHash))
	if err != nil {
		return newError(ErrTxSigning, "unable to retrieve redeem script", err)
	}

	class, _, nRequired, err := btcscript.ExtractPkScriptAddrs(redeemScript, mgr.Net())
	if err != nil {
		return newError(ErrTxSigning, "unparseable redeem script", err)
	}
	if class != btcscript.MultiSigTy {
		return newError(
			ErrTxSigning, fmt.Sprintf("redeem script is not multi-sig: %v", class), nil)
	}
	if len(sigs) < nRequired {
		errStr := fmt.Sprintf(
			"not enough signatures; need %d but got only %d", nRequired, len(sigs))
		return newError(ErrTxSigning, errStr, nil)
	}

	// Construct the unlocking script.
	// Start with an OP_0 because of the bug in bitcoind, then add nRequired signatures.
	unlockingScript := btcscript.NewScriptBuilder().AddOp(btcscript.OP_FALSE)
	for _, sig := range sigs[:nRequired] {
		unlockingScript.AddData(sig)
	}

	// Combine the redeem script and the unlocking script to get the actual signature script.
	sigScript := unlockingScript.AddData(redeemScript)
	tx.TxIn[idx].SignatureScript = sigScript.Script()

	if err := validateSigScript(tx, idx, pkScript); err != nil {
		return err
	}
	return nil
}

// validateSigScripts executes the signature script of the tx input with the
// given index, returning an error if it fails.
func validateSigScript(msgtx *btcwire.MsgTx, idx int, pkScript []byte) error {
	flags := btcscript.ScriptCanonicalSignatures | btcscript.ScriptStrictMultiSig | btcscript.ScriptBip16
	txIn := msgtx.TxIn[idx]
	engine, err := btcscript.NewScript(txIn.SignatureScript, pkScript, idx, msgtx, flags)
	if err != nil {
		return newError(ErrTxSigning, "cannot create script engine", err)
	}
	if err = engine.Execute(); err != nil {
		return newError(ErrTxSigning, "cannot validate tx signature", err)
	}
	return nil
}

// calculateSize returns an estimate of the serialized size (in bytes) of the
// given transaction. It assumes all tx inputs are P2SH multi-sig.
func calculateSize(tx *withdrawalTx) int {
	msgtx := tx.toMsgTx()
	// Assume that there will always be a change output, for simplicity. We
	// simulate that by simply copying the first output as all we care about is
	// the size of its serialized form, which should be the same for all of them
	// as they're either P2PKH or P2SH..
	if !tx.hasChange() {
		msgtx.AddTxOut(msgtx.TxOut[0])
	}
	// Craft a SignatureScript with dummy signatures for every input in this tx
	// so that we can use msgtx.SerializeSize() to get its size and don't need
	// to rely on estimations. Notice that we use 73 as the signature length as
	// that's the maximum length they may have[1]. Because of that the size
	// returned here can be up to 2 bytes bigger for every signature in every
	// input.
	// [1] https://en.bitcoin.it/wiki/Elliptic_Curve_Digital_Signature_Algorithm
	dummySig := bytes.Repeat([]byte{1}, 73)
	for i, txin := range msgtx.TxIn {
		addr := tx.inputs[i].Address()
		sigScript := btcscript.NewScriptBuilder().AddOp(btcscript.OP_FALSE)
		for c := 0; c < int(addr.Series().reqSigs); c++ {
			sigScript.AddData(dummySig)
		}
		sigScript.AddData(addr.RedeemScript())
		txin.SignatureScript = sigScript.Script()
	}
	return msgtx.SerializeSize()
}
