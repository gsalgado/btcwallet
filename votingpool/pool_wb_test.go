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
	"fmt"
	"testing"

	"github.com/btcsuite/btcwallet/waddrmgr"
)

func TestSerializationErrors(t *testing.T) {
	tearDown, mgr, _ := TstCreatePool(t)
	defer tearDown()

	tests := []struct {
		version  uint32
		pubKeys  []string
		privKeys []string
		reqSigs  uint32
		err      ErrorCode
	}{
		{
			version: 2,
			pubKeys: TstPubKeys[0:3],
			err:     ErrSeriesVersion,
		},
		{
			pubKeys: []string{"NONSENSE"},
			// Not a valid length public key.
			err: ErrSeriesStorage,
		},
		{
			pubKeys:  TstPubKeys[0:3],
			privKeys: TstPrivKeys[0:1],
			// The number of public and private keys should be the same.
			err: ErrSeriesStorage,
		},
		{
			pubKeys:  TstPubKeys[0:1],
			privKeys: []string{"NONSENSE"},
			// Not a valid length private key.
			err: ErrSeriesStorage,
		},
	}

	// We need to unlock the manager in order to encrypt with the
	// private key.
	TstUnlockManager(t, mgr)

	active := true
	for testNum, test := range tests {
		encryptedPubs, err := encryptKeys(test.pubKeys, mgr, waddrmgr.CKTPublic)
		if err != nil {
			t.Fatalf("Test #%d - Error encrypting pubkeys: %v", testNum, err)
		}
		encryptedPrivs, err := encryptKeys(test.privKeys, mgr, waddrmgr.CKTPrivate)
		if err != nil {
			t.Fatalf("Test #%d - Error encrypting privkeys: %v", testNum, err)
		}

		row := &dbSeriesRow{
			version:           test.version,
			active:            active,
			reqSigs:           test.reqSigs,
			pubKeysEncrypted:  encryptedPubs,
			privKeysEncrypted: encryptedPrivs}
		_, err = serializeSeriesRow(row)

		TstCheckError(t, fmt.Sprintf("Test #%d", testNum), err, test.err)
	}
}

func TestSerialization(t *testing.T) {
	tearDown, mgr, _ := TstCreatePool(t)
	defer tearDown()

	tests := []struct {
		version  uint32
		active   bool
		pubKeys  []string
		privKeys []string
		reqSigs  uint32
	}{
		{
			version: 1,
			active:  true,
			pubKeys: TstPubKeys[0:1],
			reqSigs: 1,
		},
		{
			version:  0,
			active:   false,
			pubKeys:  TstPubKeys[0:1],
			privKeys: TstPrivKeys[0:1],
			reqSigs:  1,
		},
		{
			pubKeys:  TstPubKeys[0:3],
			privKeys: []string{TstPrivKeys[0], "", ""},
			reqSigs:  2,
		},
		{
			pubKeys: TstPubKeys[0:5],
			reqSigs: 3,
		},
		{
			pubKeys:  TstPubKeys[0:7],
			privKeys: []string{"", TstPrivKeys[1], "", TstPrivKeys[3], "", "", ""},
			reqSigs:  4,
		},
	}

	// We need to unlock the manager in order to encrypt with the
	// private key.
	TstUnlockManager(t, mgr)

	for testNum, test := range tests {
		encryptedPubs, err := encryptKeys(test.pubKeys, mgr, waddrmgr.CKTPublic)
		if err != nil {
			t.Fatalf("Test #%d - Error encrypting pubkeys: %v", testNum, err)
		}
		encryptedPrivs, err := encryptKeys(test.privKeys, mgr, waddrmgr.CKTPrivate)
		if err != nil {
			t.Fatalf("Test #%d - Error encrypting privkeys: %v", testNum, err)
		}

		row := &dbSeriesRow{
			version:           test.version,
			active:            test.active,
			reqSigs:           test.reqSigs,
			pubKeysEncrypted:  encryptedPubs,
			privKeysEncrypted: encryptedPrivs}
		serialized, err := serializeSeriesRow(row)
		if err != nil {
			t.Fatalf("Test #%d - Error in serialization %v", testNum, err)
		}

		row, err = deserializeSeriesRow(serialized)
		if err != nil {
			t.Fatalf("Test #%d - Failed to deserialize %v %v", testNum, serialized, err)
		}

		if row.version != test.version {
			t.Errorf("Serialization #%d - version mismatch: got %d want %d",
				testNum, row.version, test.version)
		}

		if row.active != test.active {
			t.Errorf("Serialization #%d - active mismatch: got %d want %d",
				testNum, row.active, test.active)
		}

		if row.reqSigs != test.reqSigs {
			t.Errorf("Serialization #%d - row reqSigs off. Got %d, want %d",
				testNum, row.reqSigs, test.reqSigs)
		}

		if len(row.pubKeysEncrypted) != len(test.pubKeys) {
			t.Errorf("Serialization #%d - Wrong no. of pubkeys. Got %d, want %d",
				testNum, len(row.pubKeysEncrypted), len(test.pubKeys))
		}

		for i, encryptedPub := range encryptedPubs {
			got := string(row.pubKeysEncrypted[i])

			if got != string(encryptedPub) {
				t.Errorf("Serialization #%d - Pubkey deserialization. Got %v, want %v",
					testNum, got, string(encryptedPub))
			}
		}

		if len(row.privKeysEncrypted) != len(row.pubKeysEncrypted) {
			t.Errorf("Serialization #%d - no. privkeys (%d) != no. pubkeys (%d)",
				testNum, len(row.privKeysEncrypted), len(row.pubKeysEncrypted))
		}

		for i, encryptedPriv := range encryptedPrivs {
			got := string(row.privKeysEncrypted[i])

			if got != string(encryptedPriv) {
				t.Errorf("Serialization #%d - Privkey deserialization. Got %v, want %v",
					testNum, got, string(encryptedPriv))
			}
		}
	}
}

func TestDeserializationErrors(t *testing.T) {
	tearDown, _, _ := TstCreatePool(t)
	defer tearDown()

	tests := []struct {
		serialized []byte
		err        ErrorCode
	}{
		{
			serialized: make([]byte, 1000000),
			// Too many bytes (over waddrmgr.seriesMaxSerial).
			err: ErrSeriesStorage,
		},
		{
			serialized: make([]byte, 10),
			// Not enough bytes (under waddrmgr.seriesMinSerial).
			err: ErrSeriesStorage,
		},
		{
			serialized: []byte{
				1, 0, 0, 0, // 4 bytes (version)
				0,          // 1 byte (active)
				2, 0, 0, 0, // 4 bytes (reqSigs)
				3, 0, 0, 0, // 4 bytes (nKeys)
			},
			// Here we have the constant data but are missing any public/private keys.
			err: ErrSeriesStorage,
		},
		{
			serialized: []byte{2, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
			// Unsupported version.
			err: ErrSeriesVersion,
		},
	}

	for testNum, test := range tests {
		_, err := deserializeSeriesRow(test.serialized)

		TstCheckError(t, fmt.Sprintf("Test #%d", testNum), err, test.err)
	}
}

func TestValidateAndDecryptKeys(t *testing.T) {
	tearDown, manager, pool := TstCreatePool(t)
	defer tearDown()

	rawPubKeys, err := encryptKeys(TstPubKeys[0:2], manager, waddrmgr.CKTPublic)
	if err != nil {
		t.Fatalf("Failed to encrypt public keys: %v", err)
	}

	// We need to unlock the manager in order to encrypt with the
	// private key.
	TstUnlockManager(t, manager)

	rawPrivKeys, err := encryptKeys(
		[]string{TstPrivKeys[0], ""}, manager, waddrmgr.CKTPrivate)
	if err != nil {
		t.Fatalf("Failed to encrypt private keys: %v", err)
	}

	pubKeys, privKeys, err := validateAndDecryptKeys(rawPubKeys, rawPrivKeys, pool)
	if err != nil {
		t.Fatalf("Error when validating/decrypting keys: %v", err)
	}

	if len(pubKeys) != 2 {
		t.Fatalf("Unexpected number of decrypted public keys: got %d, want 2", len(pubKeys))
	}
	if len(privKeys) != 2 {
		t.Fatalf("Unexpected number of decrypted private keys: got %d, want 2", len(privKeys))
	}

	if pubKeys[0].String() != TstPubKeys[0] || pubKeys[1].String() != TstPubKeys[1] {
		t.Fatalf("Public keys don't match: %v, %v", TstPubKeys[0], TstPubKeys[1], pubKeys)
	}

	if privKeys[0].String() != TstPrivKeys[0] || privKeys[1] != nil {
		t.Fatalf("Private keys don't match: %v, %v", []string{TstPrivKeys[0], ""}, privKeys)
	}

	neuteredKey, err := privKeys[0].Neuter()
	if err != nil {
		t.Fatalf("Unable to neuter private key: %v", err)
	}
	if pubKeys[0].String() != neuteredKey.String() {
		t.Errorf("Public key (%v) does not match neutered private key (%v)",
			pubKeys[0].String(), neuteredKey.String())
	}
}

func TestValidateAndDecryptKeysErrors(t *testing.T) {
	tearDown, manager, pool := TstCreatePool(t)
	defer tearDown()

	encryptedPubKeys, err := encryptKeys(TstPubKeys[0:1], manager, waddrmgr.CKTPublic)
	if err != nil {
		t.Fatalf("Failed to encrypt public key: %v", err)
	}

	// We need to unlock the manager in order to encrypt with the
	// private key.
	TstUnlockManager(t, manager)

	encryptedPrivKeys, err := encryptKeys(TstPrivKeys[1:2], manager, waddrmgr.CKTPrivate)
	if err != nil {
		t.Fatalf("Failed to encrypt private key: %v", err)
	}

	tests := []struct {
		rawPubKeys  [][]byte
		rawPrivKeys [][]byte
		err         ErrorCode
	}{
		{
			// Number of public keys does not match number of private keys.
			rawPubKeys:  [][]byte{[]byte(TstPubKeys[0])},
			rawPrivKeys: [][]byte{},
			err:         ErrKeysPrivatePublicMismatch,
		},
		{
			// Failure to decrypt public key.
			rawPubKeys:  [][]byte{[]byte(TstPubKeys[0])},
			rawPrivKeys: [][]byte{[]byte(TstPrivKeys[0])},
			err:         ErrCrypto,
		},
		{
			// Failure to decrypt private key.
			rawPubKeys:  encryptedPubKeys,
			rawPrivKeys: [][]byte{[]byte(TstPrivKeys[0])},
			err:         ErrCrypto,
		},
		{
			// One public and one private key, but they don't match.
			rawPubKeys:  encryptedPubKeys,
			rawPrivKeys: encryptedPrivKeys,
			err:         ErrKeyMismatch,
		},
	}

	for i, test := range tests {
		_, _, err := validateAndDecryptKeys(test.rawPubKeys, test.rawPrivKeys, pool)

		TstCheckError(t, fmt.Sprintf("Test #%d", i), err, test.err)
	}
}

func encryptKeys(keys []string, mgr *waddrmgr.Manager, keyType waddrmgr.CryptoKeyType) ([][]byte, error) {
	encryptedKeys := make([][]byte, len(keys))
	var err error
	for i, key := range keys {
		if key == "" {
			encryptedKeys[i] = nil
		} else {
			encryptedKeys[i], err = mgr.Encrypt(keyType, []byte(key))
		}
		if err != nil {
			return nil, err
		}
	}
	return encryptedKeys, nil
}
