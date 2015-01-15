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
	"reflect"
	"sort"
	"testing"

	"github.com/conformal/btcutil"
	"github.com/conformal/btcwallet/txstore"
	"github.com/conformal/btcwallet/waddrmgr"
	"github.com/conformal/btcwire"
)

var (
	minConf = 100

	// random small number of satoshis used as dustThreshold
	dustThreshold btcutil.Amount = 1e4
)

func TestGetEligibleInputs(t *testing.T) {
	tearDown, pool, store := TstCreatePoolAndTxStore(t)
	defer tearDown()

	// create some eligible inputs in a specified range.
	aRanges := []*addressRange{
		TstNewAddressRange(t, 0, 0, 3, 0, 4), TstNewAddressRange(t, 1, 0, 3, 0, 6)}
	// define two series.
	series := []TstSeriesDef{
		{ReqSigs: 2, PubKeys: TstPubKeys[1:4], SeriesID: aRanges[0].SeriesID},
		{ReqSigs: 2, PubKeys: TstPubKeys[3:6], SeriesID: aRanges[1].SeriesID},
	}
	oldChainHeight := 11112
	chainHeight := oldChainHeight + minConf + 10

	// create the series.
	TstCreateSeries(t, pool, series)

	// create all the scripts.
	scripts := createScripts(t, pool.Manager(), pool, aRanges)

	// let's make two eligible inputs pr. script/address.
	expNoEligibleInputs := 2 * len(scripts)
	eligibleAmounts := []int64{int64(dustThreshold + 1), int64(dustThreshold + 1)}
	var inputs []txstore.Credit
	for i := 0; i < len(scripts); i++ {
		blockIndex := int(i) + 1
		created := TstCreateInputsOnBlock(t, store, blockIndex, oldChainHeight,
			scripts[i], eligibleAmounts)
		inputs = append(inputs, created...)
	}

	// Call InputSelection on the range.
	totalAmount := btcutil.Amount(len(inputs)) * inputs[0].Amount()
	eligibles, err := pool.getEligibleInputs(
		store, aRanges, dustThreshold, int32(chainHeight), minConf, totalAmount)
	if err != nil {
		t.Fatal("InputSelection failed:", err)
	}

	// Check we got the expected number of eligible inputs.
	if len(eligibles) != expNoEligibleInputs {
		t.Fatalf("Wrong number of eligible inputs returned. Got: %d, want: %d.",
			len(eligibles), expNoEligibleInputs)
	}

	// Check that the returned eligibles are sorted by address.
	if !sort.IsSorted(byAddress(eligibles)) {
		t.Fatal("Eligible inputs are not sorted.")
	}

	// Check that all credits are unique
	checkUniqueness(t, eligibles)
}

func TestGetEligibleInputsAmountLimit(t *testing.T) {
	tearDown, pool, store := TstCreatePoolAndTxStore(t)
	defer tearDown()

	seriesID := uint32(0)
	aRanges := []*addressRange{TstNewAddressRange(t, seriesID, 0, 3, 0, 4)}
	TstCreateSeries(
		t, pool, []TstSeriesDef{{ReqSigs: 2, PubKeys: TstPubKeys[1:4], SeriesID: seriesID}})
	scripts := createScripts(t, pool.Manager(), pool, aRanges)
	// Create one eligible input on every address in the range above.
	oldChainHeight := 11112
	chainHeight := oldChainHeight + minConf + 10
	var inputs []txstore.Credit
	for i := 0; i < len(scripts); i++ {
		blockIndex := int(i) + 1
		created := TstCreateInputsOnBlock(t, store, blockIndex, oldChainHeight,
			scripts[i], []int64{int64(dustThreshold + 1)})
		inputs = append(inputs, created...)
	}

	// Call getEligibleInputs() with an upper amount limit of half the total of
	// all credits we created above.
	amountTotal := btcutil.Amount(len(inputs)) * (dustThreshold + 1)
	eligibles, err := pool.getEligibleInputs(
		store, aRanges, dustThreshold, int32(chainHeight), minConf, amountTotal/2)
	if err != nil {
		t.Fatal("InputSelection failed:", err)
	}

	// All our credits have the same amount, and we limited to half of their
	// total amount above, so here we should get half of the credits as
	// eligible.
	if len(eligibles) != len(inputs)/2 {
		t.Fatalf("Unexpected number of eligible inputs; got %d, want %d", len(eligibles),
			len(inputs)/2)
	}
}

func TestGetEligibleInputsFromSeries(t *testing.T) {
	teardown, mgr, pool := TstCreatePool(t)
	defer teardown()
	// create some eligible inputs in a specified range.
	aRange := TstNewAddressRange(t, 0, 0, 2, 0, 4)
	blockHeight := 11112
	currentChainHeight := blockHeight + minConf + 10
	store := txstore.New("/tmp/tx.bin")
	eligibleAmounts := []int64{int64(dustThreshold + 1), int64(dustThreshold + 1)}

	// define a series.
	series := []TstSeriesDef{{ReqSigs: 2, PubKeys: TstPubKeys[1:4], SeriesID: aRange.SeriesID}}
	TstCreateSeries(t, pool, series)

	// create all the scripts.
	scripts := createScripts(t, mgr, pool, []*addressRange{aRange})

	// Let's create two eligible inputs for each of the scripts.
	expNumberOfEligibleInputs := 2 * len(scripts)
	var inputs []txstore.Credit
	for i := 0; i < len(scripts); i++ {
		blockIndex := int(i) + 1
		created := TstCreateInputsOnBlock(t, store, blockIndex, blockHeight,
			scripts[i], eligibleAmounts)
		inputs = append(inputs, created...)
	}

	// Call InputSelection on the range.
	totalAmount := btcutil.Amount(len(inputs)) * inputs[0].Amount()
	eligibles, err := pool.getEligibleInputsFromSeries(
		store, aRange, dustThreshold, int32(currentChainHeight), minConf, totalAmount)
	if err != nil {
		t.Fatal("InputSelection failed:", err)
	}

	// Check we got the expected number of eligible inputs.
	if len(eligibles) != expNumberOfEligibleInputs {
		t.Fatalf("Wrong number of eligible inputs returned. Got: %d, want: %d.",
			len(eligibles), expNumberOfEligibleInputs)
	}

	// Check that the returned eligibles are sorted by address.
	if !sort.IsSorted(byAddress(eligibles)) {
		t.Fatal("Eligible inputs are not sorted.")
	}

	// Check that all credits are unique
	checkUniqueness(t, eligibles)
}

func TestEligibleInputsAreEligible(t *testing.T) {
	tearDown, pool, store := TstCreatePoolAndTxStore(t)
	defer tearDown()
	seriesID := uint32(0)
	branch := Branch(0)
	index := Index(0)

	// create the series
	series := []TstSeriesDef{{ReqSigs: 3, PubKeys: TstPubKeys[1:6], SeriesID: seriesID}}
	TstCreateSeries(t, pool, series)

	// Create the input.
	pkScript := TstCreatePkScript(t, pool, seriesID, branch, index)
	var chainHeight int32 = 1000
	c := TstCreateInputs(t, store, pkScript, []int64{int64(dustThreshold)})[0]

	// Make sure credits is old enough to pass the minConf check.
	c.BlockHeight = int32(100)

	if !pool.isCreditEligible(c, minConf, chainHeight, dustThreshold) {
		t.Errorf("Input is not eligible and it should be.")
	}
}

func TestNonEligibleInputsAreNotEligible(t *testing.T) {
	tearDown, pool, store1 := TstCreatePoolAndTxStore(t)
	store2, storeTearDown2 := TstCreateTxStore(t)
	defer tearDown()
	defer storeTearDown2()
	seriesID := uint32(0)
	branch := Branch(0)
	index := Index(0)

	// create the series
	series := []TstSeriesDef{{ReqSigs: 3, PubKeys: TstPubKeys[1:6], SeriesID: seriesID}}
	TstCreateSeries(t, pool, series)

	pkScript := TstCreatePkScript(t, pool, seriesID, branch, index)
	var chainHeight int32 = 1000

	// Check that credit below dustThreshold is rejected.
	c1 := TstCreateInputs(t, store1, pkScript, []int64{int64(dustThreshold - 1)})[0]
	c1.BlockHeight = int32(100) // make sure it has enough confirmations.
	if pool.isCreditEligible(c1, minConf, chainHeight, dustThreshold) {
		t.Errorf("Input is eligible and it should not be.")
	}

	// Check that a credit with not enough confirmations is rejected.
	c2 := TstCreateInputs(t, store2, pkScript, []int64{int64(dustThreshold)})[0]
	// the calculation of if it has been confirmed does this:
	// chainheigt - bh + 1 >= target, which is quite weird, but the
	// reason why I need to put 902 as *that* makes 1000 - 902 +1 = 99 >=
	// 100 false
	c2.BlockHeight = int32(902)
	if pool.isCreditEligible(c2, minConf, chainHeight, dustThreshold) {
		t.Errorf("Input is eligible and it should not be.")
	}

}

func TestAddressRange(t *testing.T) {

	// Test some valid ranges.
	one := TstNewAddressRange(t, 0, 0, 0, 0, 0)
	got := one.numAddresses()
	if got != uint64(1) {
		t.Fatalf("Wrong range. Got %d, want: %d", got, 1)
	}

	two := TstNewAddressRange(t, 0, 0, 0, 0, 1)
	got = two.numAddresses()
	if got != uint64(2) {
		t.Fatalf("Wrong range. Got %d, want: %d", got, 2)
	}

	four := TstNewAddressRange(t, 0, 0, 1, 0, 1)
	got = four.numAddresses()
	if got != uint64(4) {
		t.Fatalf("Wrong range. Got %d, want: %d", got, 4)
	}

	// Test invalid ranges.
	_, err := newAddressRange(0, 1, 0, 0, 1)
	TstCheckError(t, "", err, ErrInvalidAddressRange)

	_, err = newAddressRange(0, 0, 1, 1, 0)
	TstCheckError(t, "", err, ErrInvalidAddressRange)
}

func TestCreditInterfaceSortingByAddress(t *testing.T) {
	teardown, _, pool := TstCreatePool(t)
	defer teardown()

	series := []TstSeriesDef{
		{ReqSigs: 2, PubKeys: TstPubKeys[1:4], SeriesID: 0},
		{ReqSigs: 2, PubKeys: TstPubKeys[3:6], SeriesID: 1},
	}
	TstCreateSeries(t, pool, series)

	c0 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x00, 0x00}, 0)
	c1 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x00, 0x00}, 1)
	c2 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x00, 0x01}, 0)
	c3 := TstNewFakeCredit(t, pool, 0, 0, 0, []byte{0x01, 0x00}, 0)
	c4 := TstNewFakeCredit(t, pool, 0, 0, 1, []byte{0x00, 0x00}, 0)
	c5 := TstNewFakeCredit(t, pool, 0, 1, 0, []byte{0x00, 0x00}, 0)
	c6 := TstNewFakeCredit(t, pool, 1, 0, 0, []byte{0x00, 0x00}, 0)

	randomCredits := [][]CreditInterface{
		[]CreditInterface{c6, c5, c4, c3, c2, c1, c0},
		[]CreditInterface{c2, c1, c0, c6, c5, c4, c3},
		[]CreditInterface{c6, c4, c5, c2, c3, c0, c1},
	}

	want := []CreditInterface{c0, c1, c2, c3, c4, c5, c6}

	for _, random := range randomCredits {
		sort.Sort(byAddress(random))
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

// TstFakeCredit is a structure implementing the CreditInterface used to test
// the byAddress sorting. It exists because to test the sorting properly we need
// to be able to set the Credit's TxSha and OutputIndex.
type TstFakeCredit struct {
	addr        WithdrawalAddress
	txSha       *btcwire.ShaHash
	outputIndex uint32
	amount      btcutil.Amount
}

func (c *TstFakeCredit) String() string             { return "" }
func (c *TstFakeCredit) TxSha() *btcwire.ShaHash    { return c.txSha }
func (c *TstFakeCredit) OutputIndex() uint32        { return c.outputIndex }
func (c *TstFakeCredit) Address() WithdrawalAddress { return c.addr }
func (c *TstFakeCredit) Amount() btcutil.Amount     { return c.amount }
func (c *TstFakeCredit) TxOut() *btcwire.TxOut      { return nil }
func (c *TstFakeCredit) OutPoint() *btcwire.OutPoint {
	return &btcwire.OutPoint{Hash: *c.txSha, Index: c.outputIndex}
}

func TstNewFakeCredit(t *testing.T, pool *Pool, series uint32, index Index, branch Branch, txSha []byte, outputIdx int) *TstFakeCredit {
	var hash btcwire.ShaHash
	copy(hash[:], txSha)
	addr, err := pool.WithdrawalAddress(series, branch, index)
	if err != nil {
		t.Fatalf("WithdrawalAddress failed: %v", err)
	}
	return &TstFakeCredit{
		addr:        *addr,
		txSha:       &hash,
		outputIndex: uint32(outputIdx),
	}
}

// Compile time check that TstFakeCredit implements the
// CreditInterface.
var _ CreditInterface = (*TstFakeCredit)(nil)

func checkUniqueness(t *testing.T, credits byAddress) {
	type uniq struct {
		series      uint32
		branch      Branch
		index       Index
		hash        btcwire.ShaHash
		outputIndex uint32
	}

	uniqMap := make(map[uniq]bool)
	for _, c := range credits {
		u := uniq{
			series:      c.Address().SeriesID(),
			branch:      c.Address().Branch(),
			index:       c.Address().Index(),
			hash:        *c.TxSha(),
			outputIndex: c.OutputIndex(),
		}
		if _, exists := uniqMap[u]; exists {
			t.Fatalf("Duplicate found: %v", u)
		} else {
			uniqMap[u] = true
		}
	}
}

func createScripts(t *testing.T, mgr *waddrmgr.Manager, pool *Pool, ranges []*addressRange) [][]byte {
	var scripts [][]byte
	for _, r := range ranges {
		// create expNoAddrs number of scripts.
		expNoAddrs := r.numAddresses()
		newScripts := TstCreatePkScripts(t, pool, *r)
		if uint64(len(newScripts)) != expNoAddrs {
			t.Fatalf("Wrong number of scripts generated. Got: %d, want: %d",
				len(scripts), expNoAddrs)
		}
		scripts = append(scripts, newScripts...)
	}
	return scripts
}
