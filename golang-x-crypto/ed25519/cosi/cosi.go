// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cosi provides a basic implementation
// of collective signatures based on the Ed25519 signature scheme.
//
// Note: This package is experimental;
// do not use it (yet) in security-critical contexts.
// While collective signing is based on well-established and formally-analyzed
// cryptographic techniques, this implementation may have bugs or weaknesses,
// and both API and signature format details are subject to change.
//
// A collective signature allows many participants to
// validate and sign a message collaboratively,
// to produce a single compact multisignature that can be verified
// almost as quickly and efficiently as a normal individual signature.
// Despite their compactness, collective signatures nevertheless
// record exactly which subset of participants signed a given message,
// to tolerate unavailable participants and support arbitrary policies
// defining the required thresholds or subsets of signers
// required to produce a collective signature considered acceptable.
// For further background information on collective signatures, see the paper
// http://dedis.cs.yale.edu/dissent/papers/witness-abs.
//
// This package implements the basic cryptographic operations needed
// to create and/or verify collective signatures using the Ed25519 curve.
// This package does not provide a full distributed protocol
// to create collective signatures, however.
// An implementation of CoSi,
// the scalable collective signing protocol described in the above paper,
// may be found at https://github.com/dedis/cothority.
// We recommend using CoSi to produce collective signatures in practice,
// especially if you may eventually need to scale
// to hundreds or thousands of cosigners.
// It is possible to "hand-roll" a basic collective signing protocol
// using only the cryptographic primitives implemented in this package, however.
//
// In practice, we expect this package to be used mostly
// for verification of signatures generated by the CoSi protocol,
// in the context of client applications relying on collective signatures.
// Verifying already-generated collective signatures requires
// only the code in this package.
//
// # Public Keys for Collective Signing and Verification
//
// In a conventional signing scheme such as basic Ed25519,
// the individual signer uses a private key to sign a message,
// and verifiers use the corresponding public key to check its validity.
// Collective signing involves using a set of key pairs:
// the holders of multiple distinct private keys collaborate to sign a message,
// and to verify the resulting collective signature,
// the verifier needs to have a list of the corresponding public keys.
// The key-management considerations are mostly the same
// as for standard individual signing:
// the verifier must have reason to believe the public key list is trustworthy,
// e.g., by having obtained it from a trusted source or certificate authority.
// The difference is that the verifier need to assume that the holder
// of any single corresponding private key is trustworthy.
// Even if one or a few key-holders are compromised,
// these compromised key-holders cannot forge valid messages
// unless they meet whatever threshold requirement the verifier demands.
//
// This collective signature module uses
// exactly the same public and private keys as basic ed25519 does:
// thus, you simply use ed25519.GenerateKey to produce keypairs
// suitable for collective signing using this module.
// The Cosigners type implemented by this package
// represents a set of cosigners identified by their ed25519 public keys:
// you create such a set by calling NewCosigners with the list of public keys.
//
// The order of this public key list is arbitrary,
// but must be kept consistent between signing and verifying.
// Since not all participants will necessarily
// participate in every message signing operation,
// each collective signature includes a bit-mask indicating any cosigners
// that were missing (e.g., offline) during the production of the signature.
// Each public key in the master cosigner list corresponds
// to one bit in this "absentee" bitmask,
// in corresponding order,
// so that verifiers can tell exactly which cosigners actually signed.
// The bitmask is cryptographically bound into the signature,
// so the signature will fail to verify if someone merely flips a bit
// in attempt to pretend that an absent participant
// in fact cosigned the message.
//
// Although key-management security considerations are mostly the same
// as for individual signing schemes,
// collective signing does add one important detail to be aware of.
// In the process of collecting a set of public keys to form a cosigning group,
// if those public keys originate from mutually-distrustful parties,
// as is often desirable to maximize the security and diversity of the group,
// then it is critical that each party's public-key be self-signed.
// That is, each member must verify that every other group member
// actually knows the private key corresponding to his claimed public key.
// This is standard practice anyway in both public-key infrastructure (PKI)
// and "peer-to-peer" key management as implemented by PGP for example.
// This practice becomes even more essential in collective signing, however,
// because if a malicious participant is allowed to "claim" any public key
// without proving knowledge of its corresponding private key,
// then the participant can use so-called "related-key attacks"
// to produce signatures that appear to be signed by other group members
// but in fact were signed only by the one malicious signer.
// For further details, see the CoSi paper above,
// as well as section 3.2 of this paper:
// http://cs-www.bu.edu/~reyzin/papers/multisig.pdf.
//
// # Verifying Collective Signatures
//
// Verifying collective signatures is simple,
// and may be done offline at any time without any special protocol.
// Simply use NewCosigners to create a Cosigners object
// representing the list of cosigners identified by their public keys,
// then invoke the Verify method on this object
// to verify a signature on a particular message.
// The Verify function returns true if the collective signature is valid,
// and changes the state of the mask in the Cosigners object
// to indicate which cosigners were present or absent
// in the production of this particular collective signature.
//
// Besides checking the cryptographic validity of the signature itself,
// the Verify function also invokes a customizable policy
// to check whether the actual set of cosigners that produced the signature
// is acceptable to the verifier.
// The (conservative) default policy is that every cosigner must have signed
// in order for the collective signature signature to be considered valid.
// The verifier can adjust this policy by invoking Cosigners.SetPolicy
// before invoking Verify on the signature.
// The ThresholdPolicy function may be used to form policies that
// simply require a given threshold number of signers to have cosigned.
// The caller may express an arbitrary policy, however,
// simply by passing SetPolicy an object implementing the Policy interface.
// Such a Policy can depend in any way on the set of participating cosigners,
// as well as other state such as the particular verification context
// (e.g., how security-critical an operation the signature is being used for).
//
// Note that a collective signature in which no signers actually participated
// can technically be a valid collective signature,
// and will be accepted if the verifier calls SetPolicy(ThresholdPolicy(0))!
// This merely illustrates the importance of
// choosing the verification policy carefully.
//
// # Producing Collective Signatures
//
// Although as mentioned above we recommend using a scalable protocol
// such as CoSi to produce collective signatures in practice,
// collective signatures can also be produced
// using the signing primitives in this package.
// Collective signing is more complex than verification or individual signing
// because the collective signers must collaborate actively in the process.
// The process works as follows:
//
// 1. Some party we'll call the "leader"
// initiates the collective signing process.
// The leader could be any one of the cosigners,
// or any other designated (or elected) party.
// The leader need not hold any of the cosigners' private keys.
// The leader determines which cosigners appear to be online,
// and sends them the message to be collectively signed.
//
// 2. Each cosigner first inspects the message the leader asked to be signed,
// using message-validation logic suitable to the application.
// Cosigners need not necessarily validate the message at all
// if their purpose is merely to provide transparency
// by "witnessing" and publicly logging the signed message.
// If the cosigner is willing to sign,
// it calls the Commit function to produce a signing commitment,
// returning this commitment to the leader
// along with an indication of the cosigner's willingness to participate.
// Commitments may be used only once (for signing a particular message),
// an important security property this package strictly enforces.
//
// 3. The leader adjusts the participation mask in its Cosigners object
// to reflect the set of cosigners that are online and willing to cosign.
// The leader then calls Cosigners.AggregateCommits
// to combine the willing cosigners' commitments together,
// and sends the resulting aggregate commit to all the cosigners.
//
// 4. Each cosigner now calls the Cosign function -
// the only function in this package requiring the cosigner's PrivateKey -
// to produce its portion or "share" of the collective signature.
// The cosigner sends this signature part back to the leader.
//
// 5. Finally, the leader invokes Cosigners.AggregateSignature
// to combine the participating cosigners' signature parts
// into a full collective signature.
// The resulting collective signature may subsequently checked
// by anyone using Cosigners.Verify function as described above,
// on a Cosigners object created from an identical list of public keys.
//
// The leader must keep the participation mask in its Cosigners object
// fixed between steps 2 and 4 above;
// otherwise the collective signature it produces will fail to verify.
// If any cosigner indicates willingness in step 2
// but then changes its mind or goes offline before step 4,
// the leader must restart the signing process with an adjusted mask.
// This restart risk could be eliminated, at certain costs,
// using mechanisms not implemented in this package;
// see the CoSi paper for details.
//
// While collecting signature parts in step 4,
// the leader can verify each cosigner's individual signature part
// independently using Cosigners.VerifyPart.
// This way, if any cosigner indicates willingness to participate
// but actually produces an invalid signature part -
// whether due to software bugs or malice -
// the leader can determine which cosigner is responsible,
// raise an alarm, and restart the signing process without that cosigner.
// If VerifyPart indicates each individual signature part is valid,
// then the final collective signature produced by AggregateSignature
// will also be valid, unless the leader is buggy.
//
// The standard Ed25519 scheme for individual signing
// operates deterministically, using a cryptographic hash function internally
// to produce the Schnorr commits it needs.
// This deterministic operation has important simplicity and safety benefits
// in the individual signing case,
// but this design unfortunately does not extend readily to collective signing,
// hence the need for fresh random input in the Commit phase above.
//
// # Efficiency Considerations
//
// The Cosigners object caches some cryptographic state -
// namely the aggregate public key returned by AggregatePublicKey -
// reflecting the cosigners' public keys and the current participation bitmask.
// The SetMask and SetMaskBit functions, which change the participation bitmask,
// update the cached cryptographic state accordingly.
// As a result, both collective signing and verification operations
// are maximally efficient when a single Cosigners object is used
// multiple times in succession using the same, or a similar,
// participation bitmask.
//
// Drastically changing the bitmask therefore incurs some computational cost.
// This cost is unlikely to be particularly noticeable, however,
// unless the total number of cosigners' public keys is quite large
// (e.g., thousands),
// because updating the cached aggregate public key requires only
// an elliptic curve point addition or subtraction operation
// per cosigner added or removed.
// Point addition and subtraction operations are extremely inexpensive
// compared to scalar multiplication operations,
// which represent a constant base cost in collective signing or verification.
// These constant scalar multiplication costs will thus typically dominate
// when the list of cosigners is small.
package cosi

import (
	//"golang.org/x/crypto/ed25519"
	//"golang.org/x/crypto/ed25519/internal/edwards25519"
	"test-server/golang-x-crypto/ed25519"
	"test-server/golang-x-crypto/ed25519/internal/edwards25519"
)

// MaskBit represents one bit of a Cosigners participation bitmask,
// indicating whether a given cosigner is Enabled or Disabled.
type MaskBit bool

const (
	Enabled  MaskBit = false
	Disabled MaskBit = true
)

// Cosigners represents a group of collective signers
// identified by an immutable, ordered list of their public keys.
// In addition, the Cosigners object includes a mutable bitmask
// indicating which cosigners are to participate in a signing operation,
// and which cosigners actually participated when verifying a signature.
// Finally, a Cosigners object contains a customizable Policy
// that determines what subsets of cosigners are and aren't acceptable
// when verifying a collective signature.
//
// Since a Cosigners object contains mutable fields
// and implements no thread-safety provisions internally,
// a given Cosigners instance must be used only by one thread at a time.
type Cosigners struct {
	// list of all cosigners' public keys in internalized form
	keys []edwards25519.ExtendedGroupElement

	// bit-vector of *disabled* cosigners, byte-packed little-endian,
	// or nil impplicitly all-enabled and aggr not yet computed.
	mask []byte

	// cached aggregate of all enabled cosigners' public keys
	aggr edwards25519.ExtendedGroupElement

	// cosigner-presence policy for checking signatures
	policy Policy
}

// NewCosigners creates a new Cosigners object
// for a particular list of cosigners identified by Ed25519 public keys.
//
// The specified list of public keys remains immutable
// for the lifetime of this Cosigners object.
// Collective signature verifiers must use a public key list identical
// to the one that was used in the collective signing process,
// although the participation bitmask may change
// from one collective signature to the next.
//
// The mask parameter may be nil to enable all participants initially,
// and otherwise is an initial participation bitmask as defined in SetMask.
func NewCosigners(publicKeys []ed25519.PublicKey, mask []byte) *Cosigners {
	/* var publicKeyBytes [32]byte
	cos := &Cosigners{}
	cos.keys = make([]edwards25519.ExtendedGroupElement, len(publicKeys))
	for i, publicKey := range publicKeys {
		copy(publicKeyBytes[:], publicKey)
		if !cos.keys[i].FromBytes(&publicKeyBytes) {
			return nil
		}
	}

	// Start with an all-disabled participation mask, then set it correctly
	cos.mask = make([]byte, (len(cos.keys)+7)>>3)
	for i := range cos.mask {
		cos.mask[i] = 0xff // all disabled
	}
	cos.aggr.Zero()
	cos.SetMask(mask)

	cos.policy = fullPolicy{}
	return cos */
	var pkBytes [32]byte
	cos := &Cosigners{
		keys:   make([]edwards25519.ExtendedGroupElement, len(publicKeys)),
		mask:   make([]byte, (len(publicKeys)+7)>>3), // 0 == Enabled
		policy: fullPolicy{},                         // 모두서명 기본정책
	}

	cos.aggr.Zero() // 집계키를 에드워즈 군의 단위원으로 초기화

	for i, pk := range publicKeys {
		copy(pkBytes[:], pk)
		if !cos.keys[i].FromBytes(&pkBytes) {
			return nil // invalid public key
		}
		cos.aggr.Add(&cos.aggr, &cos.keys[i])
	}

	if mask != nil {
		cos.SetMask(mask)
	}
	return cos
}

// CountTotal returns the total number of cosigners,
// i.e., the length of the list of public keys supplied to NewCosigners.
func (cos *Cosigners) CountTotal() int {
	return len(cos.keys)
}

// CountEnabled returns the number of participants currently marked Enabled
// in the participation bitmask.
// This is always between 0 and CountTotal inclusive.
func (cos *Cosigners) CountEnabled() int {
	// Yes, we could count zero-bits much more efficiently...
	count := 0
	for i := range cos.keys {
		if cos.mask[i>>3]&(1<<uint(i&7)) == 0 {
			count++
		}
	}
	return count
}

//func (cos *Cosigners) PublicKeys() []ed25519.PublicKey {
//	return cos.keys
//}

// SetMask sets the entire participation bitmask according to the provided
// packed byte-slice interpreted in little-endian byte-order.
// That is, bits 0-7 of the first byte correspond to cosigners 0-7,
// bits 0-7 of the next byte correspond to cosigners 8-15, etc.
// Each bit is set to indicate the corresponding cosigner is disabled,
// or cleared to indicate the cosigner is enabled.
//
// If the mask provided is too short (or nil),
// SetMask conservatively interprets the bits of the missing bytes
// to be 0, or Enabled.
func (cos *Cosigners) SetMask(mask []byte) {
	masklen := len(mask)
	for i := range cos.keys {
		byt := i >> 3
		bit := byte(1) << uint(i&7)
		if (byt < masklen) && (mask[byt]&bit != 0) {
			// Participant i disabled in new mask.
			if cos.mask[byt]&bit == 0 {
				cos.mask[byt] |= bit // disable it
				cos.aggr.Sub(&cos.aggr, &cos.keys[i])
			}
		} else {
			// Participant i enabled in new mask.
			if cos.mask[byt]&bit != 0 {
				cos.mask[byt] &^= bit // enable it
				cos.aggr.Add(&cos.aggr, &cos.keys[i])
			}
		}
	}
}

// Mask returns the current cosigner disable-mask
// represented a byte-packed little-endian bit-vector.
func (cos *Cosigners) Mask() []byte {
	return append([]byte{}, cos.mask...) // return copy of internal mask
}

// MaskLen returns the length in bytes
// of a complete disable-mask for this cosigner list.
func (cos *Cosigners) MaskLen() int {
	return (len(cos.keys) + 7) >> 3
}

// SetMaskBit enables or disables the mask bit for an individual cosigner.
func (cos *Cosigners) SetMaskBit(signer int, value MaskBit) {
	byt := signer >> 3
	bit := byte(1) << uint(signer&7)
	if value == Disabled { // disable
		if cos.mask[byt]&bit == 0 { // was enabled
			cos.mask[byt] |= bit // disable it
			cos.aggr.Sub(&cos.aggr, &cos.keys[signer])
		}
	} else { // enable
		if cos.mask[byt]&bit != 0 { // was disabled
			cos.mask[byt] &^= bit
			cos.aggr.Add(&cos.aggr, &cos.keys[signer])
		}
	}
}

// MaskBit returns a boolean value indicating whether
// the indicated signer is Enabled or Disabled.
func (cos *Cosigners) MaskBit(signer int) (value MaskBit) {
	byt := signer >> 3
	bit := byte(1) << uint(signer&7)
	return (cos.mask[byt] & bit) != 0
}
