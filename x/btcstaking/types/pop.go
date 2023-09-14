package types

import (
	"encoding/hex"
	"fmt"

	"github.com/babylonchain/babylon/crypto/bip322"
	bbn "github.com/babylonchain/babylon/types"
	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/cometbft/cometbft/crypto/tmhash"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
)

// NewPoP generates a new proof of possession that sk_Babylon and sk_BTC are held by the same person
// a proof of possession contains two signatures:
// - pop.BabylonSig = sign(sk_Babylon, pk_BTC)
// - pop.BtcSig = sign(sk_BTC, pop.BabylonSig)
func NewPoP(babylonSK cryptotypes.PrivKey, btcSK *btcec.PrivateKey) (*ProofOfPossession, error) {
	pop := ProofOfPossession{
		BtcSigType: BTCSigType_BIP340, // by default, we use BIP-340 encoding for BTC signature
	}

	// generate pop.BabylonSig = sign(sk_Babylon, pk_BTC)
	btcPK := btcSK.PubKey()
	bip340PK := bbn.NewBIP340PubKeyFromBTCPK(btcPK)
	babylonSig, err := babylonSK.Sign(*bip340PK)
	if err != nil {
		return nil, err
	}
	pop.BabylonSig = babylonSig

	// generate pop.BtcSig = sign(sk_BTC, pop.BabylonSig)
	// NOTE: *schnorr.Sign has to take the hash of the message.
	// So we have to hash babylonSig before signing
	babylonSigHash := tmhash.Sum(pop.BabylonSig)
	btcSig, err := schnorr.Sign(btcSK, babylonSigHash)
	if err != nil {
		return nil, err
	}
	bip340Sig := bbn.NewBIP340SignatureFromBTCSig(btcSig)
	pop.BtcSig = bip340Sig.MustMarshal()

	return &pop, nil
}

func NewPoPFromHex(popHex string) (*ProofOfPossession, error) {
	popBytes, err := hex.DecodeString(popHex)
	if err != nil {
		return nil, err
	}
	var pop ProofOfPossession
	if err := pop.Unmarshal(popBytes); err != nil {
		return nil, err
	}
	return &pop, nil
}

func (pop *ProofOfPossession) ToHexStr() (string, error) {
	popBytes, err := pop.Marshal()
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(popBytes), nil
}

// Verify verifies the validity of PoP where Bitcoin signature is in BIP-340
// 1. verify(sig=sig_btc, pubkey=pk_btc, msg=pop.BabylonSig)?
// 2. verify(sig=pop.BabylonSig, pubkey=pk_babylon, msg=pk_btc)?
func (pop *ProofOfPossession) Verify(babylonPK cryptotypes.PubKey, bip340PK *bbn.BIP340PubKey) error {
	if pop.BtcSigType != BTCSigType_BIP340 {
		return fmt.Errorf("the Bitcoin signature in this proof of possession is not using BIP-340 encoding")
	}

	// rule 1: verify(sig=sig_btc, pubkey=pk_btc, msg=pop.BabylonSig)?
	bip340Sig, err := bbn.NewBIP340Signature(pop.BtcSig)
	if err != nil {
		return err
	}
	btcSig, err := bip340Sig.ToBTCSig()
	if err != nil {
		return err
	}
	btcPK, err := bip340PK.ToBTCPK()
	if err != nil {
		return err
	}
	// NOTE: btcSig.Verify has to take hash of the message.
	// So we have to hash babylonSig before verifying the signature
	babylonSigHash := tmhash.Sum(pop.BabylonSig)
	if !btcSig.Verify(babylonSigHash, btcPK) {
		return fmt.Errorf("failed to verify babylonSig")
	}

	// rule 2: verify(sig=pop.BabylonSig, pubkey=pk_babylon, msg=pk_btc)?
	if !babylonPK.VerifySignature(*bip340PK, pop.BabylonSig) {
		return fmt.Errorf("failed to verify pop.BabylonSig")
	}

	return nil
}

// VerifyBIP322 verifies the validity of PoP where Bitcoin signature is in BIP-322
// after decoding pop.BtcSig to bip322Sig which contains sig and address,
// 1. verify(sig=bip322Sig.Sig, address=bip322Sig.Address, msg=pop.BabylonSig)?
// 2. verify(sig=pop.BabylonSig, pubkey=babylonPK, msg=bip340PK)?
// 3. verify pop.Address corresponds to bip340PK in the given network
func (pop *ProofOfPossession) VerifyBIP322(babylonPK cryptotypes.PubKey, bip340PK *bbn.BIP340PubKey, net *chaincfg.Params) error {
	if pop.BtcSigType != BTCSigType_BIP322 {
		return fmt.Errorf("the Bitcoin signature in this proof of possession is not using BIP-322 encoding")
	}

	// unmarshal pop.BtcSig to bip322Sig
	var bip322Sig BIP322Sig
	if err := bip322Sig.Unmarshal(pop.BtcSig); err != nil {
		return nil
	}

	// rule 1: verify(sig=bip322Sig.Sig, address=bip322Sig.Address, msg=pop.BabylonSig)?
	// TODO: temporary solution for MVP purposes.
	// Eventually we need to use tmhash.Sum(pop.BabylonSig) rather than bbnSigHashHexBytes
	// ref: https://github.com/babylonchain/babylon-private/issues/80
	bbnSigHash := tmhash.Sum(pop.BabylonSig)
	bbnSigHashHex := hex.EncodeToString(bbnSigHash)
	bbnSigHashHexBytes := []byte(bbnSigHashHex)
	if err := bip322.Verify(bbnSigHashHexBytes, bip322Sig.Sig, bip322Sig.Address, net); err != nil {
		return err
	}

	// rule 2: verify(sig=pop.BabylonSig, pubkey=pk_babylon, msg=pk_btc)?
	if !babylonPK.VerifySignature(*bip340PK, pop.BabylonSig) {
		return fmt.Errorf("failed to verify pop.BabylonSig")
	}

	// TODO: rule 3: verify bip322Sig.Address corresponds to bip340PK

	return nil
}
