/*
 * Copyright (c) 2014 Conformal Systems LLC <info@conformal.com>
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

package votingpool_test

import (
	"bytes"
	"testing"

	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcutil/hdkeychain"
	vp "github.com/btcsuite/btcwallet/votingpool"
	"github.com/btcsuite/btcwire"
)

func TestWithdrawal(t *testing.T) {
	tearDown, pool, store := vp.TstCreatePoolAndTxStore(t)
	defer tearDown()
	mgr := pool.Manager()

	masters := []*hdkeychain.ExtendedKey{
		vp.TstCreateMasterKey(t, bytes.Repeat([]byte{0x00, 0x01}, 16)),
		vp.TstCreateMasterKey(t, bytes.Repeat([]byte{0x02, 0x01}, 16)),
		vp.TstCreateMasterKey(t, bytes.Repeat([]byte{0x03, 0x01}, 16))}
	def := vp.TstCreateSeriesDef(t, pool, 2, masters)
	vp.TstCreateSeries(t, pool, []vp.TstSeriesDef{def})
	// Create eligible inputs and the list of outputs we need to fulfil.
	eligible := vp.TstCreateCreditsOnSeries(t, pool, def.SeriesID, []int64{5e6, 4e6}, store)
	address1 := "34eVkREKgvvGASZW7hkgE2uNc1yycntMK6"
	address2 := "3PbExiaztsSYgh6zeMswC49hLUwhTQ86XG"
	requests := []vp.OutputRequest{
		vp.TstNewOutputRequest(t, 1, address1, 4e6, mgr.Net()),
		vp.TstNewOutputRequest(t, 2, address2, 1e6, mgr.Net()),
	}
	changeStart, err := pool.ChangeAddress(def.SeriesID, 0)
	if err != nil {
		t.Fatal(err)
	}

	// Withdrawal() should fulfil the desired outputs spending from the given inputs.
	status, sigs, err := pool.Withdrawal(0, requests, eligible, changeStart, store)
	if err != nil {
		t.Fatal(err)
	}

	// Check that all outputs were successfully fulfilled.
	checkWithdrawalOutputs(t, status, map[string]btcutil.Amount{address1: 4e6, address2: 1e6})

	// NOTE: The ntxid is deterministic so we hardcode it here, but if the test
	// or the code is changed in a way that causes the generated transaction to
	// change (e.g. different inputs/outputs), the ntxid will change too and
	// this will have to be updated.
	ntxid := "eb753083db55bd0ad2eb184bfd196a7ea8b90eaa000d9293e892999695af2519"
	txSigs := sigs[ntxid]

	// Finally we use SignTx() to construct the SignatureScripts (using the raw
	// signatures).  Must unlock the manager first as signing involves looking
	// up the redeem script, which is stored encrypted.
	vp.TstUnlockManager(t, mgr)
	sha, _ := btcwire.NewShaHashFromStr(ntxid)
	tx := store.UnminedTx(sha).MsgTx()
	if err = vp.SignTx(tx, txSigs, mgr, store); err != nil {
		t.Fatal(err)
	}
}

func checkWithdrawalOutputs(
	t *testing.T, wStatus *vp.WithdrawalStatus, amounts map[string]btcutil.Amount) {
	fulfilled := wStatus.Outputs()
	if len(fulfilled) != 2 {
		t.Fatalf("Unexpected number of outputs in WithdrawalStatus; got %d, want %d",
			len(fulfilled), 2)
	}
	for _, output := range fulfilled {
		addr := output.Address()
		amount, ok := amounts[addr]
		if !ok {
			t.Fatalf("Unexpected output addr: %s", addr)
		}

		status := output.Status()
		if status != "success" {
			t.Fatalf(
				"Unexpected status for output %v; got '%s', want 'success'", output, status)
		}

		outpoints := output.Outpoints()
		if len(outpoints) != 1 {
			t.Fatalf(
				"Unexpected number of outpoints for output %v; got %d, want 1", output,
				len(outpoints))
		}

		gotAmount := outpoints[0].Amount()
		if gotAmount != amount {
			t.Fatalf("Unexpected amount for output %v; got %v, want %v", output, gotAmount, amount)
		}
	}
}
