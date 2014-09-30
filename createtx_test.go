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
	"github.com/davecgh/go-spew/spew"
)

var (
	tx_                 = tx2
	recvSerializedTx, _ = hex.DecodeString(tx_.hex)
	recvTx, _           = btcutil.NewTxFromBytes(recvSerializedTx)
	changeAddr, _       = btcutil.DecodeAddress("muqW4gcixv58tVbSKRC5q6CRKy8RmyLgZ5", activeNet.Params)
	outAddr1, _         = btcutil.DecodeAddress("1MirQ9bwyQcGVJPwKUgapu5ouK2E2Ey4gX", activeNet.Params)
	outAddr2, _         = btcutil.DecodeAddress("12MzCDwodF9G1e7jfwLXfR164RNtx4BRVG", activeNet.Params)
)

type tx struct {
	hex     string
	privKey string
	address string
}

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
	eligible := []txstore.Credit{newTxCredit(t, recvTx)}
	bs := &keystore.BlockStamp{Height: 11111}
	var tstChangeAddress = func(bs *keystore.BlockStamp) (btcutil.Address, error) {
		return changeAddr, nil
	}

	// Create a new keystore and load tx.privKey into it.
	keys, err := keystore.New("/tmp/keys.bin", "Default acccount", []byte{0, 1},
		activeNet.Params, bs)
	if err != nil {
		t.Fatal(err)
	}
	wif, err := btcutil.DecodeWIF(tx_.privKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = keys.Unlock([]byte{0, 1}); err != nil {
		t.Fatal(err)
	}
	_, err = keys.ImportPrivateKey(wif, bs)
	if err != nil {
		t.Fatal(err)
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
	expectedOutputs := map[btcutil.Address]int64{changeAddr: 9989989, outAddr2: 1, outAddr1: 10}
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
			t.Fatalf("PkScript %v not found in msgTx.TxOut: %v", pkScript, spew.Sdump(msgTx.TxOut))
		}
	}
}

func TestCreateTxInsufficientFunds(t *testing.T) {
	cfg = &config{DisallowFree: false}
	outputs := map[string]btcutil.Amount{outAddr1.String(): 10, outAddr2.String(): 1e9}
	eligible := []txstore.Credit{newTxCredit(t, recvTx)}
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

// This tx contains 2 outputs:
// {0: 0.2597 (change, addr: mnPES3VZ2kT4VuMMhaCav4pv7aCRLTgRg6),
//  1: 0.6 (not-change, addr: mnVHyDU3eNiD4i8tDT4hwqJLZJ7LDC8W6H)}
// The privKey below is for the second output
var tx1 = tx{
	hex:     "010000000122dd6d2ac57a6dc85d7be36c482822d27462e97e0388a2dd7b5f78f006724afe000000006a47304402207ad18f796460cd1bd058c2f4981dc88f2f838fa98335d5c5e99f4c6a00bfe415022008aaae8a57ee5d082204e408003511527f432533dbc0ad70aad5fa2bd5280cfe012103df001c8b58ac42b6cbfc2223b8efaa7e9a1911e529bd2c8b7f90140079034e75ffffffff0250458c01000000001976a9144b53061f0c512efef66fe03304d1138dee9ecdcd88ac00879303000000001976a9144c7878fbf5ee97e0d469b7de6e5a191334a212b688ac00000000",
	privKey: "cVaPBac5pYj9pjZ4jbHiTpiYRS1tJohM8TeGPAUcZaWrcSQxsbkT",
	address: "mnVHyDU3eNiD4i8tDT4hwqJLZJ7LDC8W6H"}

// This tx contains 2 outputs:
// {0: 367.412 (change, addr: mgRETMuqiwxgqhHvGNvseGh5U5z3UrqTwg),
//  1: 0.51 (not-change, addr: mmA8fMT3ueM4sg6SnoY5wEahdd3Yv5coxD)}
// The privKey below is for the second output
var tx2 = tx{
	hex:     "0100000001b103b03ef9a54318a14019b94ada9996934ea1cedd6847be15824f908c88f3fa000000006b483045022100afe7d9e26dd2efea67082d006e80be8c50b8654c767b079543d282e37df6460a02202a71780dd2b5c5bdaee77b30c938538fd1c6af3e4cd6cc1a1f35c190c1e23cab012102ac4bcfe048eaa6589bbbe69fb8453729ed83f3206c8458ded17105d6610fd54fffffffff028038f28d080000001976a91409e31ebe8cc3f22b62dd2897b2b58b93ac4ff82788acc0320a03000000001976a9143de0ac733acad7fa3072344543943e0ef854ab8f88ac00000000",
	privKey: "cRD6HSRzK3ePra3gXp14V9USLECjdH3HDtydfqrnziDHtANvEtVA",
	address: "mmA8fMT3ueM4sg6SnoY5wEahdd3Yv5coxD"}

func newTxCredit(t *testing.T, tx *btcutil.Tx) txstore.Credit {
	s := txstore.New("/tmp/tx.bin")
	r, err := s.InsertTx(tx, nil)
	if err != nil {
		t.Fatal(err)
	}
	// XXX: The 1 here means the second output in tx1/tx2 above.
	credit, err := r.AddCredit(1, false)
	if err != nil {
		t.Fatal(err)
	}
	return credit
}
