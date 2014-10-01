package main

import (
	"encoding/hex"
	"reflect"
	"sort"
	"testing"

	"github.com/conformal/btcscript"
	"github.com/conformal/btcutil"
	"github.com/conformal/btcwallet/keystore"
	"github.com/conformal/btcwallet/txstore"
	"github.com/conformal/btcwire"
)

// This is a tx that transfers funds to a known privKey.
// It contains 2 outputs:
// {0: 367.412 (change, addr: mgRETMuqiwxgqhHvGNvseGh5U5z3UrqTwg),
//  1: 0.51 (not-change, addr: mmA8fMT3ueM4sg6SnoY5wEahdd3Yv5coxD)}
// The privKey below is for the second output
var txInfo = struct {
	hex, privKey string
}{
	hex:     "0100000001b103b03ef9a54318a14019b94ada9996934ea1cedd6847be15824f908c88f3fa000000006b483045022100afe7d9e26dd2efea67082d006e80be8c50b8654c767b079543d282e37df6460a02202a71780dd2b5c5bdaee77b30c938538fd1c6af3e4cd6cc1a1f35c190c1e23cab012102ac4bcfe048eaa6589bbbe69fb8453729ed83f3206c8458ded17105d6610fd54fffffffff028038f28d080000001976a91409e31ebe8cc3f22b62dd2897b2b58b93ac4ff82788acc0320a03000000001976a9143de0ac733acad7fa3072344543943e0ef854ab8f88ac00000000",
	privKey: "cRD6HSRzK3ePra3gXp14V9USLECjdH3HDtydfqrnziDHtANvEtVA"}

var (
	changeAddr, _ = btcutil.DecodeAddress("muqW4gcixv58tVbSKRC5q6CRKy8RmyLgZ5", activeNet.Params)
	outAddr1, _   = btcutil.DecodeAddress("1MirQ9bwyQcGVJPwKUgapu5ouK2E2Ey4gX", activeNet.Params)
	outAddr2, _   = btcutil.DecodeAddress("12MzCDwodF9G1e7jfwLXfR164RNtx4BRVG", activeNet.Params)
)

func Test_addOutputs(t *testing.T) {
	msgtx := btcwire.NewMsgTx()
	pairs := map[string]btcutil.Amount{outAddr1.String(): 10, outAddr2.String(): 1}
	if _, err := addOutputs(msgtx, pairs); err != nil {
		t.Fatal(err)
	}
	if len(msgtx.TxOut) != 2 {
		t.Fatalf("Expected 2 outputs, found only %d", len(msgtx.TxOut))
	}
	values := []int{int(msgtx.TxOut[0].Value), int(msgtx.TxOut[1].Value)}
	sort.Ints(values)
	if !reflect.DeepEqual(values, []int{1, 10}) {
		t.Fatalf("Expected values to be [1, 10], got: %v", values)
	}
}

func TestCreateTx(t *testing.T) {
	cfg = &config{DisallowFree: false}
	outputs := map[string]btcutil.Amount{outAddr1.String(): 10, outAddr2.String(): 1}
	eligible := []txstore.Credit{newTxCredit(t, txInfo.hex, 1)}
	bs := &keystore.BlockStamp{Height: 11111}
	keys := newKeyStore(t, txInfo.privKey, bs)
	var tstChangeAddress = func(bs *keystore.BlockStamp) (btcutil.Address, error) {
		return changeAddr, nil
	}

	tx, err := createTx(eligible, outputs, bs, defaultFeeIncrement, keys, tstChangeAddress)
	if err != nil {
		t.Fatal(err)
	}

	if tx.changeAddr.String() != changeAddr.String() {
		t.Errorf("Unexpected change address; got %v, want %v",
			tx.changeAddr.String(), changeAddr.String())
	}

	msgTx := tx.tx.MsgTx()
	if len(msgTx.TxOut) != 3 {
		t.Errorf("Unexpected number of outputs; got %d, want 3", len(msgTx.TxOut))
	}

	// Check that the outputs in the tx are what we expect. It's a bit
	// convoluted because the index of the change output is randomized.
	expectedOutputs := map[btcutil.Address]int64{changeAddr: 50989989, outAddr2: 1, outAddr1: 10}
	for addr, v := range expectedOutputs {
		pkScript, err := btcscript.PayToAddrScript(addr)
		if err != nil {
			t.Fatalf("Cannot create pkScript: %v", err)
		}
		found := false
		for _, txout := range msgTx.TxOut {
			if reflect.DeepEqual(txout.PkScript, pkScript) && txout.Value == v {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("PkScript %v not found in msgTx.TxOut: %v", pkScript, msgTx.TxOut)
		}
	}
}

func TestCreateTxInsufficientFunds(t *testing.T) {
	cfg = &config{DisallowFree: false}
	outputs := map[string]btcutil.Amount{outAddr1.String(): 10, outAddr2.String(): 1e9}
	eligible := []txstore.Credit{newTxCredit(t, txInfo.hex, 1)}
	bs := &keystore.BlockStamp{Height: 11111}
	var tstChangeAddress = func(bs *keystore.BlockStamp) (btcutil.Address, error) {
		return changeAddr, nil
	}

	_, err := createTx(eligible, outputs, bs, defaultFeeIncrement, nil, tstChangeAddress)

	if err == nil {
		t.Error("Expected InsufficientFunds, got no error")
	} else if _, ok := err.(InsufficientFunds); !ok {
		t.Errorf("Unexpected error, got %v, want InsufficientFunds", err)
	}
}

// newKeyStore creates a new keystore and imports the given privKey into it.
func newKeyStore(t *testing.T, privKey string, bs *keystore.BlockStamp) *keystore.Store {
	passphrase := []byte{0, 1}
	keys, err := keystore.New("/tmp/keys.bin", "Default acccount", passphrase,
		activeNet.Params, bs)
	if err != nil {
		t.Fatal(err)
	}
	wif, err := btcutil.DecodeWIF(privKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = keys.Unlock(passphrase); err != nil {
		t.Fatal(err)
	}
	_, err = keys.ImportPrivateKey(wif, bs)
	if err != nil {
		t.Fatal(err)
	}
	return keys
}

func newTxCredit(t *testing.T, txHex string, idx uint32) txstore.Credit {
	serialized, err := hex.DecodeString(txHex)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := btcutil.NewTxFromBytes(serialized)
	if err != nil {
		t.Fatal(err)
	}
	s := txstore.New("/tmp/tx.bin")
	r, err := s.InsertTx(tx, nil)
	if err != nil {
		t.Fatal(err)
	}
	credit, err := r.AddCredit(idx, false)
	if err != nil {
		t.Fatal(err)
	}
	return credit
}
