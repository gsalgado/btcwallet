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

package votingpool_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/conformal/btcutil"
	vp "github.com/conformal/btcwallet/votingpool"
	"github.com/conformal/btcwire"
)

// TestCreditInterfaceSort checks that the sorting algorithm correctly
// sorts lexicographically by series, index, branch, txid,
// outputindex.
func TestCreditInterfaceSort(t *testing.T) {
	teardown, _, pool := vp.TstCreatePool(t)
	defer teardown()

	// Create the series 0 and 1 as they are needed for creaing the
	// fake credits.
	series := []vp.TstSeriesDef{
		{ReqSigs: 2, PubKeys: vp.TstPubKeys[1:4], SeriesID: 0},
		{ReqSigs: 2, PubKeys: vp.TstPubKeys[3:6], SeriesID: 1},
	}
	vp.TstCreateSeries(t, pool, series)

	c0 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x00, 0x00}, 0)
	c1 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x00, 0x00}, 1)
	c2 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x00, 0x01}, 0)
	c3 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x01, 0x00}, 0)
	c4 := TstNewFakeCredit(t, pool, 0, 0, 1, []byte{0x00, 0x00}, 0)
	c5 := TstNewFakeCredit(t, pool, 0, 1, 0, []byte{0x00, 0x00}, 0)
	c6 := TstNewFakeCredit(t, pool, 1, 0, 0, []byte{0x00, 0x00}, 0)

	randomCredits := []vp.Credits{
		vp.Credits{c6, c5, c4, c3, c2, c1, c0},
		vp.Credits{c2, c1, c0, c6, c5, c4, c3},
		vp.Credits{c6, c4, c5, c2, c3, c0, c1},
	}

	want := vp.Credits{c0, c1, c2, c3, c4, c5, c6}

	for _, random := range randomCredits {
		sort.Sort(random)
		got := random

		if len(got) != len(want) {
			t.Fatalf("Sorted credit slice size wrong: Got: %d, want: %d",
				len(got), len(want))
		}

		for idx := 0; idx < len(want); idx++ {
			if !reflect.DeepEqual(got[idx], want[idx]) {
				t.Errorf("Wrong output index. Got: %v, want: %v",
					got[idx], want[idx])
			}
		}
	}
}

func TestAddressRange(t *testing.T) {
	one := vp.AddressRange{
		SeriesID:    0,
		StartBranch: 0,
		StopBranch:  0,
		StartIndex:  0,
		StopIndex:   0,
	}
	two := vp.AddressRange{
		SeriesID:    0,
		StartBranch: 0,
		StopBranch:  0,
		StartIndex:  0,
		StopIndex:   1,
	}
	four := vp.AddressRange{
		SeriesID:    0,
		StartBranch: 0,
		StopBranch:  1,
		StartIndex:  0,
		StopIndex:   1,
	}

	invalidBranch := vp.AddressRange{
		StartBranch: 1,
		StopBranch:  0,
	}

	invalidIndex := vp.AddressRange{
		StartIndex: 1,
		StopIndex:  0,
	}

	got, err := one.NumAddresses()
	if err != nil {
		t.Fatalf("NumAddresses failed: %v", err)
	}
	exp := uint64(1)
	if got != exp {
		t.Fatalf("Wrong range. Got %d, want: %d", got, exp)
	}
	got, err = two.NumAddresses()
	if err != nil {
		t.Fatalf("NumAddresses failed: %v", err)
	}
	exp = 2
	if got != exp {
		t.Fatalf("Wrong range. Got %d, want: %d", got, exp)
	}
	got, err = four.NumAddresses()
	if err != nil {
		t.Fatalf("NumAddresses failed: %v", err)
	}
	exp = 4
	if got != exp {
		t.Fatalf("Wrong range. Got %d, want: %d", got, exp)
	}

	// Finally test invalid ranges
	got, err = invalidIndex.NumAddresses()
	if err == nil {
		t.Fatalf("Expected failure, but got nil")
	}
	got, err = invalidBranch.NumAddresses()
	if err == nil {
		t.Fatalf("Expected failure, but got nil")
	}
}

// TstFakeCredit is a structure implementing the CreditInterface used
// for testing purposes.
type TstFakeCredit struct {
	addr        vp.WithdrawalAddress
	txid        *btcwire.ShaHash
	outputIndex uint32
	amount      btcutil.Amount
}

func (c *TstFakeCredit) String() string {
	return ""
}

func (c *TstFakeCredit) TxSha() *btcwire.ShaHash {
	return c.txid
}

func (c *TstFakeCredit) OutputIndex() uint32 {
	return c.outputIndex
}

func (c *TstFakeCredit) Address() vp.WithdrawalAddress {
	return c.addr
}

func (c *TstFakeCredit) Amount() btcutil.Amount {
	return c.amount
}

func (c *TstFakeCredit) TxOut() *btcwire.TxOut {
	return nil
}

func (c *TstFakeCredit) OutPoint() *btcwire.OutPoint {
	return &btcwire.OutPoint{Hash: *c.txid, Index: c.outputIndex}
}

func (c *TstFakeCredit) SetAmount(amount btcutil.Amount) *TstFakeCredit {
	c.amount = amount
	return c
}

func TstNewFakeCredit(t *testing.T, pool *vp.Pool, series uint32, index vp.Index, branch vp.Branch, txid []byte, outputIdx int) *TstFakeCredit {
	var hash btcwire.ShaHash
	copy(hash[:], txid)
	addr, err := pool.WithdrawalAddress(series, branch, index)
	if err != nil {
		t.Fatalf("WithdrawalAddress failed: %v", err)
	}
	return &TstFakeCredit{
		addr:        *addr,
		txid:        &hash,
		outputIndex: uint32(outputIdx),
	}
}

// Compile time check that TstFakeCredit implements the
// CreditInterface.
var _ vp.CreditInterface = (*TstFakeCredit)(nil)
