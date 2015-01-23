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

	"github.com/btcsuite/btcnet"
	"github.com/btcsuite/btcutil"
	"github.com/btcsuite/btcwallet/txstore"
	"github.com/btcsuite/btcwire"
)

const eligibleInputMinConfirmations = 100

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
		next, err := nextAddr(p, addr.seriesID, addr.branch, addr.index, lastSeriesID+1)
		if err != nil {
			return nil, newError(
				ErrInputSelection, "failed to get next withdrawal address", err)
		} else if next == nil {
			log.Debugf(
				"getEligibleInputs: reached last addr (%s), stopping", addr.AddrIdentifier())
			finished = true
		}
		addr = next
	}
	return inputs, nil
}

// nextAddr returns the next WithdrawalAddress according to the input selection
// rules: http://opentransactions.org/wiki/index.php/Input_Selection_Algorithm_(voting_pools)
// It returns nil if the new address' seriesID is >= stopSeriesID.
func nextAddr(p *Pool, seriesID uint32, branch Branch, index Index, stopSeriesID uint32) (
	*WithdrawalAddress, error) {
	branch++
	series := p.GetSeries(seriesID)
	if series == nil {
		return nil, newError(ErrSeriesNotExists, fmt.Sprintf("unknown seriesID: %d", seriesID), nil)
	}
	if int(branch) > len(series.publicKeys) {
		highestIdx, err := p.highestUsedSeriesIndex(seriesID)
		if err != nil {
			return nil, err
		}
		if index > highestIdx {
			seriesID++
			log.Debugf("nextAddr(): reached last branch (%d) and highest used index (%d), "+
				"moving on to next series (%d)", branch, index, seriesID)
			index = 0
		} else {
			index++
		}
		branch = 0
	}

	if seriesID >= stopSeriesID {
		return nil, nil
	}

	addr, err := p.WithdrawalAddress(seriesID, branch, index)
	if err != nil && err.(Error).ErrorCode == ErrWithdrawFromUnusedAddr {
		// The used indices will vary between branches so sometimes we'll try to
		// get a WithdrawalAddress that hasn't been used before, and in such
		// cases we just need to move on to the next one.
		log.Debugf("nextAddr(): skipping addr (series #%d, branch #%d, index #%d) as it hasn't "+
			"been used before", seriesID, branch, index)
		return nextAddr(p, seriesID, branch, index, stopSeriesID)
	}
	return addr, err
}

// highestUsedSeriesIndex returns the highest index among all of this Pool's
// used addresses for the given seriesID. It returns 0 if there are no used
// addresses with the given seriesID.
func (p *Pool) highestUsedSeriesIndex(seriesID uint32) (Index, error) {
	maxIdx := Index(0)
	series := p.GetSeries(seriesID)
	if series == nil {
		return maxIdx,
			newError(ErrSeriesNotExists, fmt.Sprintf("unknown seriesID: %d", seriesID), nil)
	}
	for i := range series.publicKeys {
		idx, err := p.highestUsedIndexFor(seriesID, Branch(i))
		if err != nil {
			return Index(0), err
		}
		if idx > maxIdx {
			maxIdx = idx
		}
	}
	return maxIdx, nil
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
func (p *Pool) isCreditEligible(c txstore.Credit, minConf int, chainHeight int32, dustThreshold btcutil.Amount) bool {
	if c.Amount() < dustThreshold {
		return false
	}
	if !c.Confirmed(minConf, chainHeight) {
		return false
	}
	if p.isCharterOutput(c) {
		return false
	}

	return true
}

// isCharterOutput - TODO: In order to determine this, we need the txid
// and the output index of the current charter output, which we don't have yet.
func (p *Pool) isCharterOutput(c txstore.Credit) bool {
	return false
}
