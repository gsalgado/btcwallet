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
	"testing"

	vp "github.com/conformal/btcwallet/votingpool"
)

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
