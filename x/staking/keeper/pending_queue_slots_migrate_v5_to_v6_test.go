package keeper_test

import (
	"time"

	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

// Can be removed once migration v6 is complete

// TestPopulateValidatorQueuePendingFromIterator_NoEntries tests migration with 0 entries
func (s *KeeperTestSuite) TestPopulateValidatorQueuePendingFromIterator_NoEntries() {
	err := s.stakingKeeper.PopulateValidatorQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

// TestPopulateValidatorQueuePendingFromIterator_SingleEntry tests migration with 1 entry
func (s *KeeperTestSuite) TestPopulateValidatorQueuePendingFromIterator_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)

	// Set up old format queue entry directly in store (pre-migration format)
	err := stakingkeeper.SetValidatorQueueEntryPreV6Migration(s.stakingKeeper, s.ctx, testTime, testHeight, []string{"cosmosvaloper1abc123"})
	s.Require().NoError(err)

	// Run migration
	err = s.stakingKeeper.PopulateValidatorQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	// Verify pending slots were populated
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0].Time)
	s.Require().Equal(testHeight, slots[0].Height)
}

// TestPopulateValidatorQueuePendingFromIterator_MultipleEntries tests migration with multiple entries
func (s *KeeperTestSuite) TestPopulateValidatorQueuePendingFromIterator_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	testHeights := []int64{100, 200, 300}

	// Set up old format queue entries directly in store (pre-migration format)
	for i, t := range testTimes {
		err := stakingkeeper.SetValidatorQueueEntryPreV6Migration(s.stakingKeeper, s.ctx, t, testHeights[i], []string{"cosmosvaloper1abc123"})
		s.Require().NoError(err)
	}

	// Run migration
	err := s.stakingKeeper.PopulateValidatorQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	// Verify pending slots were populated
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)

	// Slots should be sorted by time, then height
	s.Require().Equal(testTimes[0], slots[0].Time)
	s.Require().Equal(testHeights[0], slots[0].Height)
	s.Require().Equal(testTimes[1], slots[1].Time)
	s.Require().Equal(testHeights[1], slots[1].Height)
	s.Require().Equal(testTimes[2], slots[2].Time)
	s.Require().Equal(testHeights[2], slots[2].Height)
}

// TestPopulateUBDQueuePendingFromIterator_NoEntries tests migration with 0 entries
func (s *KeeperTestSuite) TestPopulateUBDQueuePendingFromIterator_NoEntries() {
	err := s.stakingKeeper.PopulateUBDQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

// TestPopulateUBDQueuePendingFromIterator_SingleEntry tests migration with 1 entry
func (s *KeeperTestSuite) TestPopulateUBDQueuePendingFromIterator_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Set up old format queue entry directly in store (pre-migration format)
	err := stakingkeeper.SetUBDQueueEntryPreV6Migration(s.stakingKeeper, s.ctx, testTime)
	s.Require().NoError(err)

	// Run migration
	err = s.stakingKeeper.PopulateUBDQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	// Verify pending slots were populated
	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0])
}

// TestPopulateUBDQueuePendingFromIterator_MultipleEntries tests migration with multiple entries
func (s *KeeperTestSuite) TestPopulateUBDQueuePendingFromIterator_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}

	// Set up old format queue entries directly in store (pre-migration format)
	for _, t := range testTimes {
		err := stakingkeeper.SetUBDQueueEntryPreV6Migration(s.stakingKeeper, s.ctx, t)
		s.Require().NoError(err)
	}

	// Run migration
	err := s.stakingKeeper.PopulateUBDQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	// Verify pending slots were populated
	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)

	// Slots should be sorted by time
	s.Require().Equal(testTimes[0], slots[0])
	s.Require().Equal(testTimes[1], slots[1])
	s.Require().Equal(testTimes[2], slots[2])
}

// TestPopulateRedelegationQueuePendingFromIterator_NoEntries tests migration with 0 entries
func (s *KeeperTestSuite) TestPopulateRedelegationQueuePendingFromIterator_NoEntries() {
	err := s.stakingKeeper.PopulateRedelegationQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

// TestPopulateRedelegationQueuePendingFromIterator_SingleEntry tests migration with 1 entry
func (s *KeeperTestSuite) TestPopulateRedelegationQueuePendingFromIterator_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Set up old format queue entry directly in store (pre-migration format)
	err := stakingkeeper.SetRedelegationQueueEntryPreV6Migration(s.stakingKeeper, s.ctx, testTime)
	s.Require().NoError(err)

	// Run migration
	err = s.stakingKeeper.PopulateRedelegationQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	// Verify pending slots were populated
	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0])
}

// TestPopulateRedelegationQueuePendingFromIterator_MultipleEntries tests migration with multiple entries
func (s *KeeperTestSuite) TestPopulateRedelegationQueuePendingFromIterator_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}

	// Set up old format queue entries directly in store (pre-migration format)
	for _, t := range testTimes {
		err := stakingkeeper.SetRedelegationQueueEntryPreV6Migration(s.stakingKeeper, s.ctx, t)
		s.Require().NoError(err)
	}

	// Run migration
	err := s.stakingKeeper.PopulateRedelegationQueuePendingFromIterator(s.ctx)
	s.Require().NoError(err)

	// Verify pending slots were populated
	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)

	// Slots should be sorted by time
	s.Require().Equal(testTimes[0], slots[0])
	s.Require().Equal(testTimes[1], slots[1])
	s.Require().Equal(testTimes[2], slots[2])
}
