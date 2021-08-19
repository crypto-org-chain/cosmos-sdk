package keyring

import (
	"strings"
	"testing"

	"github.com/99designs/keyring"
	"github.com/stretchr/testify/suite"

	"github.com/cosmos/cosmos-sdk/codec/legacy"
	"github.com/cosmos/cosmos-sdk/crypto/hd"

	"github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const n1 = "cosmos"


type MigrationTestSuite struct {
	suite.Suite

	kb Keyring
	priv cryptotypes.PrivKey
	pub cryptotypes.PubKey
	ks keystore

}

func (s *MigrationTestSuite) SetupSuite() {
	dir := s.T().TempDir()
	mockIn := strings.NewReader("")
	cdc := getCodec()

	kb, err := New(n1, BackendTest, dir, mockIn, cdc)
	s.Require().NoError(err)

	ks, ok := kb.(keystore)
	s.Require().True(ok)

	s.kb = kb
	s.ks = ks

	s.priv = cryptotypes.PrivKey(secp256k1.GenPrivKey())
	s.pub = s.priv.PubKey()

}

func (s *MigrationTestSuite) TestMigrateLegacyLocalKey() {

	legacyLocalInfo := newLegacyLocalInfo(n1, s.pub, string(legacy.Cdc.MustMarshal(s.priv)), hd.Secp256k1.Name())
	serializedLegacyLocalInfo := MarshalInfo(legacyLocalInfo)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedLegacyLocalInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	_, migrated, err := s.ks.migrate(n1)
	s.Require().True(migrated)
	s.Require().NoError(err)
}


func (s *MigrationTestSuite) TestMigrateLegacyLedgerKey() {

	account, coinType, index := uint32(118), uint32(0), uint32(0)
	hdPath := hd.NewFundraiserParams(account, coinType, index)
	legacyLedgerInfo := newLegacyLedgerInfo(n1, s.pub, *hdPath, hd.Secp256k1.Name())
	serializedLegacyLedgerInfo := MarshalInfo(legacyLedgerInfo)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedLegacyLedgerInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	_, migrated, err := s.ks.migrate(n1)
	s.Require().True(migrated)
	s.Require().NoError(err)
}

func (s *MigrationTestSuite) TestMigrateLegacyOfflineKey() {


	legacyOfflineInfo := newLegacyOfflineInfo(n1, s.pub, hd.Secp256k1.Name())
	serializedLegacyOfflineInfo := MarshalInfo(legacyOfflineInfo)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedLegacyOfflineInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	_, migrated, err := s.ks.migrate(n1)
	s.Require().True(migrated)
	s.Require().NoError(err)
}

func (s *MigrationTestSuite) TestMigrateLegacyMultiKey(t *testing.T) {

	multi := multisig.NewLegacyAminoPubKey(
		1, []cryptotypes.PubKey{
			s.pub,
		},
	)

	LegacyMultiInfo, err := NewLegacyMultiInfo(n1, multi)
	s.Require().NoError(err)
	serializedLegacyMultiInfo := MarshalInfo(LegacyMultiInfo)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedLegacyMultiInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	_, migrated, err := s.ks.migrate(n1)
	s.Require().True(migrated)
	s.Require().NoError(err)
}

func (s *MigrationTestSuite) TestMigrateLocalRecord() {
	
	k1, err := NewLocalRecord("test record", s.priv, s.pub)
	s.Require().NoError(err)

	serializedRecord, err := s.ks.protoMarshalRecord(k1)
	s.Require().NoError(err)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedRecord,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	k2, migrated, err := s.ks.migrate(n1)
	s.Require().Equal(k2.Name, k1.Name)
	
	pub, err := k2.GetPubKey()
	s.Require().NoError(err)
	s.Require().Equal(pub, s.pub)

	priv, err := extractPrivKeyFromRecord(k2)
	s.Require().NoError(err)
	s.Require().Equal(priv, s.priv)

	s.Require().False(migrated)
	s.Require().NoError(err)
}

func (s *MigrationTestSuite) TestMigrateOneRandomItemError() {

	randomBytes := []byte("abckd0s03l")

	errItem := keyring.Item{
		Key:         n1,
		Data:        randomBytes,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(errItem))

	_, migrated, err := s.ks.migrate(n1)
	s.Require().False(migrated)
	s.Require().Error(err)
}

func (s *MigrationTestSuite) TestMigrateAllLegacyMultiOffline() {
	
	multi := multisig.NewLegacyAminoPubKey(
		1, []cryptotypes.PubKey{
			s.pub,
		},
	)

	LegacyMultiInfo, err := NewLegacyMultiInfo(n1, multi)
	s.Require().NoError(err)
	serializedLegacyMultiInfo := MarshalInfo(LegacyMultiInfo)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedLegacyMultiInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	legacyOfflineInfo := newLegacyOfflineInfo(n1, s.pub, hd.Secp256k1.Name())
	serializedLegacyOfflineInfo := MarshalInfo(legacyOfflineInfo)

	item = keyring.Item{
		Key:         n1,
		Data:        serializedLegacyOfflineInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	migrated, err := s.kb.MigrateAll()
	s.Require().True(migrated)
	s.Require().NoError(err)
}

func (s *MigrationTestSuite) TestMigrateAllNoItem() {
	migrated, err := s.kb.MigrateAll()
	s.Require().False(migrated)
	s.Require().NoError(err)
}

func (s *MigrationTestSuite) TestMigrateErrUnknownItemKey() {

	legacyOfflineInfo := newLegacyOfflineInfo(n1, s.pub, hd.Secp256k1.Name())
	serializedLegacyOfflineInfo := MarshalInfo(legacyOfflineInfo)

	item := keyring.Item{
		Key:         n1,
		Data:        serializedLegacyOfflineInfo,
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	incorrectItemKey := n1 + "1"
	_, migrated, err := s.ks.migrate(incorrectItemKey)
	s.Require().False(migrated)
	s.Require().EqualError(err, sdkerrors.Wrap(sdkerrors.ErrKeyNotFound, incorrectItemKey).Error())
}

func (s *MigrationTestSuite) TestMigrateErrEmptyItemData(t *testing.T) {
	item := keyring.Item{
		Key:         n1,
		Data:        []byte{},
		Description: "SDK kerying version",
	}

	s.Require().NoError(s.ks.SetItem(item))

	_, migrated, err := s.ks.migrate(n1)
	s.Require().False(migrated)
	s.Require().EqualError(err, sdkerrors.Wrap(sdkerrors.ErrKeyNotFound, n1).Error())
}

// newLegacyLocalInfo creates a new legacyLocalInfo instance
func newLegacyLocalInfo(name string, pub cryptotypes.PubKey, privArmor string, algo hd.PubKeyType) LegacyInfo {
	return &legacyLocalInfo{
		Name:         name,
		PubKey:       pub,
		PrivKeyArmor: privArmor,
		Algo:         algo,
	}
}

// newLegacyOfflineInfo creates a new legacyLedgerInfo instance
func newLegacyLedgerInfo(name string, pub cryptotypes.PubKey, path hd.BIP44Params, algo hd.PubKeyType) LegacyInfo {
	return &legacyLedgerInfo{
		Name:   name,
		PubKey: pub,
		Path:   path,
		Algo:   algo,
	}
}

// newLegacyOfflineInfo creates a new legacyOfflineInfo instance
func newLegacyOfflineInfo(name string, pub cryptotypes.PubKey, algo hd.PubKeyType) LegacyInfo {
	return &legacyOfflineInfo{
		Name:   name,
		PubKey: pub,
		Algo:   algo,
	}
}
