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

package wtxmgr

import "fmt"

// ErrorCode identifies a category of error.
type ErrorCode uint8

// These constants are used to identify a specific TxStoreError.
const (
	// ErrDatabase indicates an error with the underlying database.  When
	// this error code is set, the Err field of the StoreError will be
	// set to the underlying error returned from the database.
	ErrDatabase ErrorCode = iota

	// ErrData describes an error where data stored in the transaction
	// database is incorrect.  This may be due to missing values, values of
	// wrong sizes, or data from different buckets that is inconsistent with
	// itself.  Recovering from an ErrData requires rebuilding all
	// transaction history or manual database surgery.  If the failure was
	// not due to data corruption, this error category indicates a
	// programming error in this package.
	ErrData

	// ErrInput describes an error where the variables passed into this
	// function by the caller are obviously incorrect.  Examples include
	// passing transactions which do not serialize, or attempting to insert
	// a credit at an index for which no transaction output exists.
	ErrInput
)

var errStrs = [...]string{
	ErrDatabase: "ErrDatabase",
	ErrData:     "ErrData",
	ErrInput:    "ErrInput",
}

// String returns the ErrorCode as a human-readable name.
func (e ErrorCode) String() string {
	if e < ErrorCode(len(errStrs)) {
		return errStrs[e]
	}
	return fmt.Sprintf("ErrorCode(%d)", e)
}

// StoreError provides a single type for errors that can happen during tx
// store operation. It is similar to waddrmgr.ManagerError.
type StoreError struct {
	Code ErrorCode // Describes the kind of error
	Desc string    // Human readable description of the issue
	Err  error     // Underlying error, optional
}

// Error satisfies the error interface and prints human-readable errors.
func (e StoreError) Error() string {
	if e.Err != nil {
		return e.Desc + ": " + e.Err.Error()
	}
	return e.Desc
}

func storeError(c ErrorCode, desc string, err error) StoreError {
	return StoreError{Code: c, Desc: desc, Err: err}
}
