package votingpool

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/btcsuite/btcnet"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/txstore"
	"github.com/btcsuite/btcwire"
)

// CreditInterface is an abstraction over credits used in a voting
// pool.
type CreditInterface interface {
	fmt.Stringer
	TxSha() *btcwire.ShaHash
	OutputIndex() uint32
	Address() WithdrawalAddress
	Amount() btcutil.Amount
	OutPoint() *btcwire.OutPoint
	TxOut() *btcwire.TxOut
}

// Credit implements the CreditInterface.
type Credit struct {
	txstore.Credit
	addr WithdrawalAddress
}

// NewCredit initialises a new Credit.
func NewCredit(credit txstore.Credit, addr WithdrawalAddress) *Credit {
	return &Credit{Credit: credit, addr: addr}
}

func (c *Credit) String() string {
	return fmt.Sprintf("Credit of %v to %v", c.Amount(), c.Address())
}

// TxSha returns the sha hash of the underlying transaction.
func (c *Credit) TxSha() *btcwire.ShaHash {
	return c.Credit.TxRecord.Tx().Sha()
}

// OutputIndex returns the outputindex of the ouput in the underlying
// transaction.
func (c *Credit) OutputIndex() uint32 {
	return c.Credit.OutputIndex
}

// Address returns the voting pool address.
func (c *Credit) Address() WithdrawalAddress {
	return c.addr
}

// Compile time check that Credit implements CreditInterface.
var _ CreditInterface = (*Credit)(nil)

// byAddress defines the methods needed to satisify sort.Interface to sort a
// slice of CreditInterfaces by their address.
type byAddress []CreditInterface

func (c byAddress) Len() int      { return len(c) }
func (c byAddress) Swap(i, j int) { c[i], c[j] = c[j], c[i] }

// Less returns true if the element at positions i is smaller than the
// element at position j. The 'smaller-than' relation is defined to be
// the lexicographic ordering defined on the tuple (SeriesID, Index,
// Branch, TxSha, OutputIndex).
func (c byAddress) Less(i, j int) bool {
	if c[i].Address().seriesID < c[j].Address().seriesID {
		return true
	}

	if c[i].Address().seriesID == c[j].Address().seriesID &&
		c[i].Address().index < c[j].Address().index {
		return true
	}

	if c[i].Address().seriesID == c[j].Address().seriesID &&
		c[i].Address().index == c[j].Address().index &&
		c[i].Address().branch < c[j].Address().branch {
		return true
	}

	txidComparison := bytes.Compare(c[i].TxSha().Bytes(), c[j].TxSha().Bytes())

	if c[i].Address().seriesID == c[j].Address().seriesID &&
		c[i].Address().index == c[j].Address().index &&
		c[i].Address().branch == c[j].Address().branch &&
		txidComparison < 0 {
		return true
	}

	if c[i].Address().seriesID == c[j].Address().seriesID &&
		c[i].Address().index == c[j].Address().index &&
		c[i].Address().branch == c[j].Address().branch &&
		txidComparison == 0 &&
		c[i].OutputIndex() < c[j].OutputIndex() {
		return true
	}
	return false
}

// getEligibleInputs returns eligible inputs with addresses between startAddress
// and the last used address of lastSeriesID.
func (p *Pool) getEligibleInputs(
	store *txstore.Store,
	startAddress *WithdrawalAddress,
	lastSeriesID uint32,
	dustThreshold btcutil.Amount,
	chainHeight int32,
	minConf int,
	limit btcutil.Amount) ([]CreditInterface, error) {

	if p.GetSeries(lastSeriesID) == nil {
		str := fmt.Sprintf("lastSeriesID (%d) does not exist", lastSeriesID)
		return nil, newError(ErrSeriesNotExists, str, nil)
	}
	unspents, err := store.UnspentOutputs()
	if err != nil {
		return nil, newError(ErrInputSelection, "failed to get unspent outputs", err)
	}
	addrMap, err := groupCreditsByAddr(unspents, p.manager.Net())
	if err != nil {
		return nil, newError(ErrInputSelection, "grouping credits by address failed", err)
	}
	var inputs []CreditInterface
	finished := false
	addr := startAddress
	totalAmount := btcutil.Amount(0)
	for !finished {
		log.Debugf("Looking for eligible inputs at address %v", addr.AddrIdentifier())
		if candidates, ok := addrMap[addr.Addr().EncodeAddress()]; ok {
			var eligibles []CreditInterface
			for _, c := range candidates {
				if p.isCreditEligible(c, minConf, chainHeight, dustThreshold) {
					eligibles = append(eligibles, NewCredit(c, *addr))
					totalAmount += c.Amount()
					if totalAmount >= limit {
						log.Debugf("getEligibleInputs: reached amount limit (%d), stopping", limit)
						finished = true
						break
					}
				}
			}
			// Make sure the eligibles are correctly sorted.
			sort.Sort(byAddress(eligibles))
			inputs = append(inputs, eligibles...)
		}
		nextAddr, err := addr.NextBefore(lastSeriesID + 1)
		if err != nil {
			return nil, newError(
				ErrInputSelection, "failed to get next withdrawal address", err)
		} else if nextAddr == nil {
			log.Debugf(
				"getEligibleInputs: reached last addr (%s), stopping", addr.AddrIdentifier())
			finished = true
		}
		addr = nextAddr
	}
	return inputs, nil
}

// groupCreditsByAddr converts a slice of credits to a map from the
// string representation of an encoded address to the unspent outputs
// associated with that address.
func groupCreditsByAddr(credits []txstore.Credit, net *btcnet.Params) (map[string][]txstore.Credit, error) {
	addrMap := make(map[string][]txstore.Credit)
	for _, c := range credits {
		_, addrs, _, err := c.Addresses(net)
		if err != nil {
			return nil, err
		}
		// As our credits are all P2SH we should never have more
		// than one address per credit, so let's error out if that
		// assumption is violated.
		if len(addrs) != 1 {
			return nil, newError(ErrInputSelection, "credit has more than one address", nil)
		}
		encAddr := addrs[0].EncodeAddress()
		if v, ok := addrMap[encAddr]; ok {
			addrMap[encAddr] = append(v, c)
		} else {
			addrMap[encAddr] = []txstore.Credit{c}
		}
	}

	return addrMap, nil
}

// isCreditEligible tests a given credit for eligibilty with respect
// to number of confirmations, the dust threshold and that it is not
// the charter output.
func (vp *Pool) isCreditEligible(c txstore.Credit, minConf int, chainHeight int32, dustThreshold btcutil.Amount) bool {
	if c.Amount() < dustThreshold {
		return false
	}
	if !c.Confirmed(minConf, chainHeight) {
		return false
	}
	if vp.isCharterOutput(c) {
		return false
	}

	return true
}

// isCharterOutput - TODO: In order to determine this, we need the txid
// and the output index of the current charter output, which we don't have yet.
func (vp *Pool) isCharterOutput(c txstore.Credit) bool {
	return false
}
