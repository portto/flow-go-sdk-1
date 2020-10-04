/*
 * Flow Go SDK
 *
 * Copyright 2019-2020 Dapper Labs, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package flow

import (
	"fmt"
	"sort"

	"github.com/onflow/cadence"
	jsoncdc "github.com/onflow/cadence/encoding/json"

	"github.com/portto/blocto-flow-go-sdk/crypto"
)

// A Transaction is a full transaction object containing a payload and signatures.
type Transaction struct {
	// Script is the UTF-8 encoded Cadence source code that defines the execution logic for this transaction.
	Script []byte

	// Arguments is a list of Cadence values passed into this transaction.
	//
	// Each argument is encoded as JSON-CDC bytes.
	Arguments [][]byte

	// ReferenceBlockID is a reference to the block used to calculate the expiry of this transaction.
	//
	// A transaction is considered expired if it is submitted to Flow after refBlock + N, where N
	// is a constant defined by the network.
	//
	// For example, if a transaction references a block with height of X and the network limit is 10,
	// a block with height X+10 is the last block that is allowed to include this transaction.
	ReferenceBlockID Identifier

	// GasLimit is the maximum number of computational units that can be used to execute this transaction.
	GasLimit uint64

	// ProposalKey is the account key used to propose this transaction.
	//
	// A proposal key references a specific key on an account, along with an up-to-date
	// sequence number for that key. This sequence number is used to prevent replay attacks.
	//
	// You can find more information about sequence numbers here: https://docs.onflow.org/concepts/transaction-signing/#sequence-numbers
	ProposalKey ProposalKey

	// Payer is the account that pays the fee for this transaction.
	//
	// You can find more information about the payer role here: https://docs.onflow.org/concepts/transaction-signing/#signer-roles
	Payer Address

	// Authorizers is a list of the accounts that are authorizing this transaction to
	// mutate to their on-chain account state.
	//
	// You can find more information about the authorizer role here: https://docs.onflow.org/concepts/transaction-signing/#signer-roles
	Authorizers []Address

	// PayloadSignatures is a list of signatures generated by the proposer and authorizer roles.
	//
	// A payload signature is generated over the inner portion of the transaction (payload).
	//
	// You can find more information about transaction signatures here: https://docs.onflow.org/concepts/transaction-signing/#anatomy-of-a-transaction
	PayloadSignatures []TransactionSignature

	// EnvelopeSignatures is a list of signatures generated by the payer role.
	//
	// A payload signature is generated over the outer portion of the transaction (payload + payloadSignatures).
	//
	// You can find more information about transaction signatures here: https://docs.onflow.org/concepts/transaction-signing/#anatomy-of-a-transaction
	EnvelopeSignatures []TransactionSignature
}

// NewTransaction initializes and returns an empty transaction.
func NewTransaction() *Transaction {
	return &Transaction{}
}

// ID returns the canonical SHA3-256 hash of this transaction.
func (t *Transaction) ID() Identifier {
	return HashToID(defaultEntityHasher.ComputeHash(t.Encode()))
}

// SetScript sets the Cadence script for this transaction.
//
// The script is the UTF-8 encoded Cadence source code.
func (t *Transaction) SetScript(script []byte) *Transaction {
	t.Script = script
	return t
}

// AddArgument adds a Cadence argument to this transaction.
func (t *Transaction) AddArgument(arg cadence.Value) error {
	encodedArg, err := jsoncdc.Encode(arg)
	if err != nil {
		return fmt.Errorf("failed to encode argument: %w", err)
	}

	t.Arguments = append(t.Arguments, encodedArg)
	return nil
}

// AddRawArgument adds a raw JSON-CDC encoded argument to this transaction.
func (t *Transaction) AddRawArgument(arg []byte) *Transaction {
	t.Arguments = append(t.Arguments, arg)
	return t
}

// Argument returns the decoded argument at the given index.
func (t *Transaction) Argument(i int) (cadence.Value, error) {
	if i < 0 {
		return nil, fmt.Errorf("argument index must be positive")
	}

	if i >= len(t.Arguments) {
		return nil, fmt.Errorf("no argument at index %d", i)
	}

	encodedArg := t.Arguments[i]

	arg, err := jsoncdc.Decode(encodedArg)
	if err != nil {
		return nil, fmt.Errorf("failed to decode argument at index %d: %w", i, err)
	}

	return arg, nil
}

// SetReferenceBlockID sets the reference block ID for this transaction.
//
// A transaction is considered expired if it is submitted to Flow after refBlock + N, where N
// is a constant defined by the network.
//
// For example, if a transaction references a block with height of X and the network limit is 10,
// a block with height X+10 is the last block that is allowed to include this transaction.
func (t *Transaction) SetReferenceBlockID(blockID Identifier) *Transaction {
	t.ReferenceBlockID = blockID
	return t
}

// SetGasLimit sets the gas limit for this transaction.
func (t *Transaction) SetGasLimit(limit uint64) *Transaction {
	t.GasLimit = limit
	return t
}

// SetProposalKey sets the proposal key and sequence number for this transaction.
//
// The first two arguments specify the account key to be used, and the last argument is the sequence
// number being declared.
func (t *Transaction) SetProposalKey(address Address, keyIndex int, sequenceNum uint64) *Transaction {
	proposalKey := ProposalKey{
		Address:        address,
		KeyIndex:       keyIndex,
		SequenceNumber: sequenceNum,
	}
	t.ProposalKey = proposalKey
	return t
}

// SetPayer sets the payer account for this transaction.
func (t *Transaction) SetPayer(address Address) *Transaction {
	t.Payer = address
	return t
}

// AddAuthorizer adds an authorizer account to this transaction.
func (t *Transaction) AddAuthorizer(address Address) *Transaction {
	t.Authorizers = append(t.Authorizers, address)
	return t
}

// signerList returns a list of unique accounts required to sign this transaction.
//
// The list is returned in the following order:
// 1. PROPOSER
// 2. PAYER
// 2. AUTHORIZERS (in insertion order)
//
// The only exception to the above ordering is for deduplication; if the same account
// is used in multiple signing roles, only the first occurrence is included in the list.
func (t *Transaction) signerList() []Address {
	signers := make([]Address, 0)
	seen := make(map[Address]struct{})

	var addSigner = func(address Address) {
		_, ok := seen[address]
		if ok {
			return
		}

		signers = append(signers, address)
		seen[address] = struct{}{}
	}

	if t.ProposalKey.Address != EmptyAddress {
		addSigner(t.ProposalKey.Address)
	}

	if t.Payer != EmptyAddress {
		addSigner(t.Payer)
	}

	for _, authorizer := range t.Authorizers {
		addSigner(authorizer)
	}

	return signers
}

// signerMap returns a mapping from address to signer index.
func (t *Transaction) signerMap() map[Address]int {
	signers := make(map[Address]int)

	for i, signer := range t.signerList() {
		signers[signer] = i
	}

	return signers
}

// SignPayload signs the transaction payload with the specified account key.
//
// The resulting signature is combined with the account address and key index before
// being added to the transaction.
//
// This function returns an error if the signature cannot be generated.
func (t *Transaction) SignPayload(address Address, keyIndex int, signer crypto.Signer) error {
	sig, err := signer.Sign(t.PayloadMessage())
	if err != nil {
		// TODO: wrap error
		return err
	}

	t.AddPayloadSignature(address, keyIndex, sig)

	return nil
}

// SignEnvelope signs the full transaction (payload + payload signatures) with the specified account key.
//
// The resulting signature is combined with the account address and key index before
// being added to the transaction.
//
// This function returns an error if the signature cannot be generated.
func (t *Transaction) SignEnvelope(address Address, keyIndex int, signer crypto.Signer) error {
	sig, err := signer.Sign(t.EnvelopeMessage())
	if err != nil {
		// TODO: wrap error
		return err
	}

	t.AddEnvelopeSignature(address, keyIndex, sig)

	return nil
}

// AddPayloadSignature adds a payload signature to the transaction for the given address and key index.
func (t *Transaction) AddPayloadSignature(address Address, keyIndex int, sig []byte) *Transaction {
	s := t.createSignature(address, keyIndex, sig)

	t.PayloadSignatures = append(t.PayloadSignatures, s)
	sort.Slice(t.PayloadSignatures, compareSignatures(t.PayloadSignatures))

	return t
}

// AddEnvelopeSignature adds an envelope signature to the transaction for the given address and key index.
func (t *Transaction) AddEnvelopeSignature(address Address, keyIndex int, sig []byte) *Transaction {
	s := t.createSignature(address, keyIndex, sig)

	t.EnvelopeSignatures = append(t.EnvelopeSignatures, s)
	sort.Slice(t.EnvelopeSignatures, compareSignatures(t.EnvelopeSignatures))

	return t
}

func (t *Transaction) createSignature(address Address, keyIndex int, sig []byte) TransactionSignature {
	signerIndex, signerExists := t.signerMap()[address]
	if !signerExists {
		signerIndex = -1
	}

	return TransactionSignature{
		Address:     address,
		SignerIndex: signerIndex,
		KeyIndex:    keyIndex,
		Signature:   sig,
	}
}

func (t *Transaction) PayloadMessage() []byte {
	temp := t.payloadCanonicalForm()
	return mustRLPEncode(&temp)
}

func (t *Transaction) payloadCanonicalForm() interface{} {
	authorizers := make([][]byte, len(t.Authorizers))
	for i, auth := range t.Authorizers {
		authorizers[i] = auth.Bytes()
	}

	return struct {
		Script                    []byte
		Arguments                 [][]byte
		ReferenceBlockID          []byte
		GasLimit                  uint64
		ProposalKeyAddress        []byte
		ProposalKeyIndex          uint64
		ProposalKeySequenceNumber uint64
		Payer                     []byte
		Authorizers               [][]byte
	}{
		Script:                    t.Script,
		Arguments:                 t.Arguments,
		ReferenceBlockID:          t.ReferenceBlockID[:],
		GasLimit:                  t.GasLimit,
		ProposalKeyAddress:        t.ProposalKey.Address.Bytes(),
		ProposalKeyIndex:          uint64(t.ProposalKey.KeyIndex),
		ProposalKeySequenceNumber: t.ProposalKey.SequenceNumber,
		Payer:                     t.Payer.Bytes(),
		Authorizers:               authorizers,
	}
}

// EnvelopeMessage returns the signable message for the transaction envelope.
//
// This message is only signed by the payer account.
func (t *Transaction) EnvelopeMessage() []byte {
	temp := t.envelopeCanonicalForm()
	return mustRLPEncode(&temp)
}

func (t *Transaction) envelopeCanonicalForm() interface{} {
	return struct {
		Payload           interface{}
		PayloadSignatures interface{}
	}{
		Payload:           t.payloadCanonicalForm(),
		PayloadSignatures: signaturesList(t.PayloadSignatures).canonicalForm(),
	}
}

// Encode serializes the full transaction data including the payload and all signatures.
func (t *Transaction) Encode() []byte {
	temp := struct {
		Payload            interface{}
		PayloadSignatures  interface{}
		EnvelopeSignatures interface{}
	}{
		Payload:            t.payloadCanonicalForm(),
		PayloadSignatures:  signaturesList(t.PayloadSignatures).canonicalForm(),
		EnvelopeSignatures: signaturesList(t.EnvelopeSignatures).canonicalForm(),
	}

	return mustRLPEncode(&temp)
}

// DecodeFromBytes un-serializes from raw data to the full transaction data
func (t *Transaction) DecodeFromBytes(bs []byte) error {
	type payload struct {
		Script                    []byte
		Arguments                 [][]byte
		ReferenceBlockID          []byte
		GasLimit                  uint64
		ProposalKeyAddress        []byte
		ProposalKeyID             uint64
		ProposalKeySequenceNumber uint64
		Payer                     []byte
		Authorizers               [][]byte
	}

	type signature struct {
		SignerIndex uint
		KeyID       uint
		Signature   []byte
	}

	type tempStruct struct {
		Payload            payload
		PayloadSignatures  []signature
		EnvelopeSignatures []signature
	}

	temp := tempStruct{}
	if err := rlpDecode(bs, &temp); err != nil {
		return err
	}

	t.Script = temp.Payload.Script
	var tempReferenceBlockID [32]byte
	copy(tempReferenceBlockID[:], temp.Payload.ReferenceBlockID)
	t.ReferenceBlockID = tempReferenceBlockID
	t.GasLimit = temp.Payload.GasLimit
	var tempProposalKeyAddress [8]byte
	copy(tempProposalKeyAddress[:], temp.Payload.ProposalKeyAddress)
	t.ProposalKey.Address = tempProposalKeyAddress
	t.ProposalKey.KeyIndex = int(temp.Payload.ProposalKeyID)
	t.ProposalKey.SequenceNumber = temp.Payload.ProposalKeySequenceNumber
	var tempAddress [8]byte
	copy(tempAddress[:], temp.Payload.ProposalKeyAddress)
	var tempPayer [8]byte
	copy(tempPayer[:], temp.Payload.Payer)
	t.Payer = tempPayer
	t.Arguments = temp.Payload.Arguments

	t.Authorizers = make([]Address, len(temp.Payload.Authorizers))
	for i, auth := range temp.Payload.Authorizers {
		var tempAuth [8]byte
		copy(tempAuth[:], auth)
		t.Authorizers[i] = tempAddress
	}

	t.PayloadSignatures = make([]TransactionSignature, len(temp.PayloadSignatures))
	for i, sig := range temp.PayloadSignatures {
		t.PayloadSignatures[i] = TransactionSignature{
			Address:     t.signerList()[sig.SignerIndex],
			SignerIndex: int(sig.SignerIndex),
			KeyIndex:    int(sig.KeyID),
			Signature:   sig.Signature,
		}
	}

	t.EnvelopeSignatures = make([]TransactionSignature, len(temp.EnvelopeSignatures))
	for i, sig := range temp.EnvelopeSignatures {
		t.EnvelopeSignatures[i] = TransactionSignature{
			Address:     t.signerList()[sig.SignerIndex],
			SignerIndex: int(sig.SignerIndex),
			KeyIndex:    int(sig.KeyID),
			Signature:   sig.Signature,
		}
	}

	return nil
}

// DecodeFromPayloadBytes un-serializes from payload raw data to the full transaction data
func (t *Transaction) DecodeFromPayloadBytes(bs []byte) error {
	type payload struct {
		Script                    []byte
		Arguments                 [][]byte
		ReferenceBlockID          []byte
		GasLimit                  uint64
		ProposalKeyAddress        []byte
		ProposalKeyID             uint64
		ProposalKeySequenceNumber uint64
		Payer                     []byte
		Authorizers               [][]byte
	}

	type signature struct {
		SignerIndex uint
		KeyID       uint
		Signature   []byte
	}

	type tempStruct struct {
		Payload           payload
		PayloadSignatures []signature
	}

	temp := tempStruct{}
	if err := rlpDecode(bs, &temp); err != nil {
		return err
	}

	t.Script = temp.Payload.Script
	var tempReferenceBlockID [32]byte
	copy(tempReferenceBlockID[:], temp.Payload.ReferenceBlockID)
	t.ReferenceBlockID = tempReferenceBlockID
	t.GasLimit = temp.Payload.GasLimit
	var tempProposalKeyAddress [8]byte
	copy(tempProposalKeyAddress[:], temp.Payload.ProposalKeyAddress)
	t.ProposalKey.Address = tempProposalKeyAddress
	t.ProposalKey.KeyIndex = int(temp.Payload.ProposalKeyID)
	t.ProposalKey.SequenceNumber = temp.Payload.ProposalKeySequenceNumber
	var tempAddress [8]byte
	copy(tempAddress[:], temp.Payload.ProposalKeyAddress)
	var tempPayer [8]byte
	copy(tempPayer[:], temp.Payload.Payer)
	t.Payer = tempPayer
	t.Arguments = temp.Payload.Arguments

	t.Authorizers = make([]Address, len(temp.Payload.Authorizers))
	for i, auth := range temp.Payload.Authorizers {
		var tempAuth [8]byte
		copy(tempAuth[:], auth)
		t.Authorizers[i] = tempAddress
	}

	t.PayloadSignatures = make([]TransactionSignature, len(temp.PayloadSignatures))
	for i, sig := range temp.PayloadSignatures {
		t.PayloadSignatures[i] = TransactionSignature{
			Address:     t.signerList()[sig.SignerIndex],
			SignerIndex: int(sig.SignerIndex),
			KeyIndex:    int(sig.KeyID),
			Signature:   sig.Signature,
		}
	}

	return nil
}

// A ProposalKey is the key that specifies the proposal key and sequence number for a transaction.
type ProposalKey struct {
	Address        Address
	KeyIndex       int
	SequenceNumber uint64
}

// A TransactionSignature is a signature associated with a specific account key.
type TransactionSignature struct {
	Address     Address
	SignerIndex int
	KeyIndex    int
	Signature   []byte
}

func (s TransactionSignature) canonicalForm() interface{} {
	return struct {
		SignerIndex uint
		KeyIndex    uint
		Signature   []byte
	}{
		SignerIndex: uint(s.SignerIndex), // int is not RLP-serializable
		KeyIndex:    uint(s.KeyIndex),    // int is not RLP-serializable
		Signature:   s.Signature,
	}
}

func compareSignatures(signatures []TransactionSignature) func(i, j int) bool {
	return func(i, j int) bool {
		sigA := signatures[i]
		sigB := signatures[j]
		return sigA.SignerIndex < sigB.SignerIndex || sigA.KeyIndex < sigB.KeyIndex
	}
}

type signaturesList []TransactionSignature

func (s signaturesList) canonicalForm() interface{} {
	signatures := make([]interface{}, len(s))

	for i, signature := range s {
		signatures[i] = signature.canonicalForm()
	}

	return signatures
}

type TransactionResult struct {
	Status TransactionStatus
	Error  error
	Events []Event
}

// TransactionStatus represents the status of a transaction.
type TransactionStatus int

const (
	// TransactionStatusUnknown indicates that the transaction status is not known.
	TransactionStatusUnknown TransactionStatus = iota
	// TransactionStatusPending is the status of a pending transaction.
	TransactionStatusPending
	// TransactionStatusFinalized is the status of a finalized transaction.
	TransactionStatusFinalized
	// TransactionStatusExecuted is the status of an executed transaction.
	TransactionStatusExecuted
	// TransactionStatusSealed is the status of a sealed transaction.
	TransactionStatusSealed
	// TransactionStatusExpired is the status of an expired transaction.
	TransactionStatusExpired
)

// String returns the string representation of a transaction status.
func (s TransactionStatus) String() string {
	return [...]string{"UNKNOWN", "PENDING", "FINALIZED", "EXECUTED", "SEALED", "EXPIRED"}[s]
}
