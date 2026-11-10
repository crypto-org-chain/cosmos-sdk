package keeper_test

import (
	"bytes"
	"encoding/binary"
	"time"

	"cosmossdk.io/core/header"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

func (s *KeeperTestSuite) TestGetValidatorQueuePendingSlots_NoEntries() {
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Nil(slots)
}

func (s *KeeperTestSuite) TestGetValidatorQueuePendingSlots_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)

	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime, testHeight)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0].Time)
	s.Require().Equal(testHeight, slots[0].Height)
}

func (s *KeeperTestSuite) TestGetValidatorQueuePendingSlots_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}
	testHeights := []int64{100, 200, 300}

	for i, t := range testTimes {
		err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, t, testHeights[i])
		s.Require().NoError(err)
	}

	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)
	// Should be sorted by time, then height
	for i, slot := range slots {
		s.Require().Equal(testTimes[i], slot.Time)
		s.Require().Equal(testHeights[i], slot.Height)
	}
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_EmptySlice() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)

	// Add a slot first
	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime, testHeight)
	s.Require().NoError(err)

	// Set empty slice (should delete)
	err = s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, []stakingtypes.TimeHeightQueueSlot{})
	s.Require().NoError(err)

	// Verify it's deleted
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_DuplicateEntries() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)

	// Set with duplicates
	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: testTime, Height: testHeight},
		{Time: testTime, Height: testHeight}, // duplicate
		{Time: testTime, Height: testHeight}, // duplicate
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	// Verify only one entry persisted
	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0].Time)
	s.Require().Equal(testHeight, retrievedSlots[0].Height)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)

	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: testTime, Height: testHeight},
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0].Time)
	s.Require().Equal(testHeight, retrievedSlots[0].Height)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_MultipleEntries() {
	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Height: 300},
		{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Height: 100},
		{Time: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Height: 200},
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 3)
	// Should be sorted by time, then height
	s.Require().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), retrievedSlots[0].Time)
	s.Require().Equal(int64(100), retrievedSlots[0].Height)
	s.Require().Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), retrievedSlots[1].Time)
	s.Require().Equal(int64(200), retrievedSlots[1].Height)
	s.Require().Equal(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), retrievedSlots[2].Time)
	s.Require().Equal(int64(300), retrievedSlots[2].Height)
}

func (s *KeeperTestSuite) TestAddValidatorQueuePendingSlot() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)

	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime, testHeight)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0].Time)
	s.Require().Equal(testHeight, slots[0].Height)

	// Adding the same slot again should not create a duplicate
	err = s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime, testHeight)
	s.Require().NoError(err)

	slots, err = s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1) // Still only one entry
}

func (s *KeeperTestSuite) TestRemoveValidatorQueuePendingSlot() {
	testTime1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight1 := int64(100)
	testTime2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	testHeight2 := int64(200)

	// Add two slots
	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime1, testHeight1)
	s.Require().NoError(err)
	err = s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime2, testHeight2)
	s.Require().NoError(err)

	// Remove one
	err = s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, testTime1, testHeight1)
	s.Require().NoError(err)

	// Verify only one remains
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime2, slots[0].Time)
	s.Require().Equal(testHeight2, slots[0].Height)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_SortingEdgeCases_SameTimeDifferentHeights() {
	// Same time, different heights - should sort by height
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: testTime, Height: 300},
		{Time: testTime, Height: 100},
		{Time: testTime, Height: 200},
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 3)
	// Should be sorted by height when times are equal
	s.Require().Equal(testTime, retrievedSlots[0].Time)
	s.Require().Equal(int64(100), retrievedSlots[0].Height)
	s.Require().Equal(testTime, retrievedSlots[1].Time)
	s.Require().Equal(int64(200), retrievedSlots[1].Height)
	s.Require().Equal(testTime, retrievedSlots[2].Time)
	s.Require().Equal(int64(300), retrievedSlots[2].Height)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_SortingEdgeCases_SameHeightDifferentTimes() {
	// Same height, different times - should sort by time first
	testHeight := int64(100)
	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), Height: testHeight},
		{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Height: testHeight},
		{Time: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Height: testHeight},
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 3)
	// Should be sorted by time first
	s.Require().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), retrievedSlots[0].Time)
	s.Require().Equal(testHeight, retrievedSlots[0].Height)
	s.Require().Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), retrievedSlots[1].Time)
	s.Require().Equal(testHeight, retrievedSlots[1].Height)
	s.Require().Equal(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), retrievedSlots[2].Time)
	s.Require().Equal(testHeight, retrievedSlots[2].Height)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_Deduplication_SameTimeDifferentHeight() {
	// Same time but different height should NOT deduplicate
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: testTime, Height: 100},
		{Time: testTime, Height: 200}, // Different height, should NOT be deduplicated
		{Time: testTime, Height: 100}, // Same time+height, should be deduplicated
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 2) // Should have 2 unique entries (100 and 200)

	// Should be sorted
	s.Require().Equal(testTime, retrievedSlots[0].Time)
	s.Require().Equal(int64(100), retrievedSlots[0].Height)
	s.Require().Equal(testTime, retrievedSlots[1].Time)
	s.Require().Equal(int64(200), retrievedSlots[1].Height)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_Deduplication_SameHeightDifferentTime() {
	// Same height but different time should NOT deduplicate
	testHeight := int64(100)
	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Height: testHeight},
		{Time: time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), Height: testHeight}, // Different time, should NOT be deduplicated
		{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), Height: testHeight}, // Same time+height, should be deduplicated
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 2) // Should have 2 unique entries (different times)

	// Should be sorted by time first
	s.Require().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), retrievedSlots[0].Time)
	s.Require().Equal(testHeight, retrievedSlots[0].Height)
	s.Require().Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), retrievedSlots[1].Time)
	s.Require().Equal(testHeight, retrievedSlots[1].Height)
}

func (s *KeeperTestSuite) TestRemoveValidatorQueuePendingSlot_FromEmptyList() {
	// Remove from empty list should be no-op, not error
	err := s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), 100)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

func (s *KeeperTestSuite) TestRemoveValidatorQueuePendingSlot_NonExistentEntry() {
	// Add one slot
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight := int64(100)
	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime, testHeight)
	s.Require().NoError(err)

	// Try to remove non-existent entry
	err = s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), 200)
	s.Require().NoError(err) // Should be no-op, not error

	// Original slot should still be there
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0].Time)
	s.Require().Equal(testHeight, slots[0].Height)
}

func (s *KeeperTestSuite) TestRemoveValidatorQueuePendingSlot_MiddleEntry() {
	// Add three slots
	testTime1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight1 := int64(100)
	testTime2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	testHeight2 := int64(200)
	testTime3 := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)
	testHeight3 := int64(300)

	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime1, testHeight1)
	s.Require().NoError(err)
	err = s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime2, testHeight2)
	s.Require().NoError(err)
	err = s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime3, testHeight3)
	s.Require().NoError(err)

	// Remove middle entry
	err = s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, testTime2, testHeight2)
	s.Require().NoError(err)

	// Verify only first and last remain
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 2)
	s.Require().Equal(testTime1, slots[0].Time)
	s.Require().Equal(testHeight1, slots[0].Height)
	s.Require().Equal(testTime3, slots[1].Time)
	s.Require().Equal(testHeight3, slots[1].Height)
}

func (s *KeeperTestSuite) TestRemoveValidatorQueuePendingSlot_SharedTimeOrHeight() {
	testTime1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testTime2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	sameTime := stakingtypes.TimeHeightQueueSlot{Time: testTime1, Height: 200}
	sameHeight := stakingtypes.TimeHeightQueueSlot{Time: testTime2, Height: 100}
	s.Require().NoError(s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, []stakingtypes.TimeHeightQueueSlot{
		{Time: testTime1, Height: 100}, sameTime, sameHeight,
	}))

	s.Require().NoError(s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, testTime1, 100))

	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Equal([]stakingtypes.TimeHeightQueueSlot{sameTime, sameHeight}, slots)
}

func (s *KeeperTestSuite) TestRemoveValidatorQueuePendingSlot_AllEntries() {
	// Add two slots
	testTime1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	testHeight1 := int64(100)
	testTime2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	testHeight2 := int64(200)

	err := s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime1, testHeight1)
	s.Require().NoError(err)
	err = s.stakingKeeper.AddValidatorQueuePendingSlot(s.ctx, testTime2, testHeight2)
	s.Require().NoError(err)

	// Remove all entries
	err = s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, testTime1, testHeight1)
	s.Require().NoError(err)
	err = s.stakingKeeper.RemoveValidatorQueuePendingSlot(s.ctx, testTime2, testHeight2)
	s.Require().NoError(err)

	// Key should be deleted (empty list)
	slots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

func (s *KeeperTestSuite) TestSetValidatorQueuePendingSlots_TimeWithNanosecondPrecision() {
	// Test with nanosecond precision
	testTime := time.Date(2024, 1, 1, 12, 34, 56, 123456789, time.UTC)
	testHeight := int64(100)

	slots := []stakingtypes.TimeHeightQueueSlot{
		{Time: testTime, Height: testHeight},
	}

	err := s.stakingKeeper.SetValidatorQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetValidatorQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0].Time) // Should preserve nanosecond precision
	s.Require().Equal(testHeight, retrievedSlots[0].Height)
}

// --- UBD Queue Tests ---

func (s *KeeperTestSuite) TestGetUBDQueuePendingSlots_NoEntries() {
	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Nil(slots)
}

func (s *KeeperTestSuite) TestGetUBDQueuePendingSlots_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	err := s.stakingKeeper.AddUBDQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0])
}

func (s *KeeperTestSuite) TestGetUBDQueuePendingSlots_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}

	for _, t := range testTimes {
		err := s.stakingKeeper.AddUBDQueuePendingSlot(s.ctx, t)
		s.Require().NoError(err)
	}

	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)
	// Should be sorted
	for i, slot := range slots {
		s.Require().Equal(testTimes[i], slot)
	}
}

func (s *KeeperTestSuite) TestSetUBDQueuePendingSlots_EmptySlice() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Add a slot first
	err := s.stakingKeeper.AddUBDQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	// Set empty slice (should delete)
	err = s.stakingKeeper.SetUBDQueuePendingSlots(s.ctx, []time.Time{})
	s.Require().NoError(err)

	// Verify it's deleted
	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

func (s *KeeperTestSuite) TestSetUBDQueuePendingSlots_DuplicateEntries() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Set with duplicates
	slots := []time.Time{
		testTime,
		testTime, // duplicate
		testTime, // duplicate
	}

	err := s.stakingKeeper.SetUBDQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	// Verify only one entry persisted
	retrievedSlots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0])
}

func (s *KeeperTestSuite) TestSetUBDQueuePendingSlots_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	slots := []time.Time{testTime}

	err := s.stakingKeeper.SetUBDQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0])
}

func (s *KeeperTestSuite) TestSetUBDQueuePendingSlots_MultipleEntries() {
	slots := []time.Time{
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	err := s.stakingKeeper.SetUBDQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 3)
	// Should be sorted
	s.Require().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), retrievedSlots[0])
	s.Require().Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), retrievedSlots[1])
	s.Require().Equal(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), retrievedSlots[2])
}

func (s *KeeperTestSuite) TestAddUBDQueuePendingSlot() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	err := s.stakingKeeper.AddUBDQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0])

	// Adding the same time again should not create a duplicate
	// Add calls Set internally, which deduplicates, so there should still be only 1 entry
	err = s.stakingKeeper.AddUBDQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	slots, err = s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1) // Still only one entry due to deduplication in Set
	s.Require().Equal(testTime, slots[0])
}

func (s *KeeperTestSuite) TestAddUBDQueuePendingSlot_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}

	// Add multiple entries
	for _, t := range testTimes {
		err := s.stakingKeeper.AddUBDQueuePendingSlot(s.ctx, t)
		s.Require().NoError(err)
	}

	// Verify all entries were added and sorted
	slots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)
	// Should be sorted
	for i, slot := range slots {
		s.Require().Equal(testTimes[i], slot)
	}
}

func (s *KeeperTestSuite) TestSetUBDQueuePendingSlots_TimeWithNanosecondPrecision() {
	// Test with nanosecond precision
	testTime := time.Date(2024, 1, 1, 12, 34, 56, 123456789, time.UTC)

	slots := []time.Time{testTime}

	err := s.stakingKeeper.SetUBDQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetUBDQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0]) // Should preserve nanosecond precision
}

// --- Redelegation Queue Tests ---

func (s *KeeperTestSuite) TestGetRedelegationQueuePendingSlots_NoEntries() {
	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Nil(slots)
}

func (s *KeeperTestSuite) TestGetRedelegationQueuePendingSlots_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	err := s.stakingKeeper.AddRedelegationQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0])
}

func (s *KeeperTestSuite) TestGetRedelegationQueuePendingSlots_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}

	for _, t := range testTimes {
		err := s.stakingKeeper.AddRedelegationQueuePendingSlot(s.ctx, t)
		s.Require().NoError(err)
	}

	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)
	// Should be sorted
	for i, slot := range slots {
		s.Require().Equal(testTimes[i], slot)
	}
}

func (s *KeeperTestSuite) TestSetRedelegationQueuePendingSlots_EmptySlice() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Add a slot first
	err := s.stakingKeeper.AddRedelegationQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	// Set empty slice (should delete)
	err = s.stakingKeeper.SetRedelegationQueuePendingSlots(s.ctx, []time.Time{})
	s.Require().NoError(err)

	// Verify it's deleted
	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Empty(slots)
}

func (s *KeeperTestSuite) TestSetRedelegationQueuePendingSlots_DuplicateEntries() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Set with duplicates
	slots := []time.Time{
		testTime,
		testTime, // duplicate
		testTime, // duplicate
	}

	err := s.stakingKeeper.SetRedelegationQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	// Verify only one entry persisted
	retrievedSlots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0])
}

func (s *KeeperTestSuite) TestSetRedelegationQueuePendingSlots_SingleEntry() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	slots := []time.Time{testTime}

	err := s.stakingKeeper.SetRedelegationQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0])
}

func (s *KeeperTestSuite) TestSetRedelegationQueuePendingSlots_MultipleEntries() {
	slots := []time.Time{
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
	}

	err := s.stakingKeeper.SetRedelegationQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 3)
	// Should be sorted
	s.Require().Equal(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), retrievedSlots[0])
	s.Require().Equal(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC), retrievedSlots[1])
	s.Require().Equal(time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC), retrievedSlots[2])
}

func (s *KeeperTestSuite) TestAddRedelegationQueuePendingSlot() {
	testTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	err := s.stakingKeeper.AddRedelegationQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1)
	s.Require().Equal(testTime, slots[0])

	// Adding the same time again should not create a duplicate
	// Add calls Set internally, which deduplicates, so there should still be only 1 entry
	err = s.stakingKeeper.AddRedelegationQueuePendingSlot(s.ctx, testTime)
	s.Require().NoError(err)

	slots, err = s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 1) // Still only one entry due to deduplication in Set
	s.Require().Equal(testTime, slots[0])
}

func (s *KeeperTestSuite) TestAddRedelegationQueuePendingSlot_MultipleEntries() {
	testTimes := []time.Time{
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC),
	}

	// Add multiple entries
	for _, t := range testTimes {
		err := s.stakingKeeper.AddRedelegationQueuePendingSlot(s.ctx, t)
		s.Require().NoError(err)
	}

	// Verify all entries were added and sorted
	slots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(slots, 3)
	// Should be sorted
	for i, slot := range slots {
		s.Require().Equal(testTimes[i], slot)
	}
}

func (s *KeeperTestSuite) TestSetRedelegationQueuePendingSlots_TimeWithNanosecondPrecision() {
	// Test with nanosecond precision
	testTime := time.Date(2024, 1, 1, 12, 34, 56, 123456789, time.UTC)

	slots := []time.Time{testTime}

	err := s.stakingKeeper.SetRedelegationQueuePendingSlots(s.ctx, slots)
	s.Require().NoError(err)

	retrievedSlots, err := s.stakingKeeper.GetRedelegationQueuePendingSlots(s.ctx)
	s.Require().NoError(err)
	s.Require().Len(retrievedSlots, 1)
	s.Require().Equal(testTime, retrievedSlots[0]) // Should preserve nanosecond precision
}

func (s *KeeperTestSuite) TestDequeueAllMatureQueues() {
	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Each entry's delegator is its completion time, so the dequeued entries can be told apart.
	delegator := func(t time.Time) string { return t.Format(time.RFC3339) }
	queues := []struct {
		name     string
		key      []byte
		entryKey func(t time.Time) []byte
		enqueue  func(ctx sdk.Context, t time.Time) error
		hasEntry func(ctx sdk.Context, t time.Time) (bool, error)
		addSlot  func(ctx sdk.Context, t time.Time) error
		slots    func(ctx sdk.Context) ([]time.Time, error)
		dequeue  func(ctx sdk.Context) (delegators []string, err error)
	}{
		{
			name:     "unbonding",
			key:      stakingtypes.UBDQueuePendingSlotsKey,
			entryKey: stakingtypes.GetUnbondingDelegationTimeKey,
			enqueue: func(ctx sdk.Context, t time.Time) error {
				return s.stakingKeeper.SetUBDQueueTimeSlice(ctx, t, []stakingtypes.DVPair{{DelegatorAddress: delegator(t), ValidatorAddress: "val"}})
			},
			hasEntry: func(ctx sdk.Context, t time.Time) (bool, error) {
				pairs, err := s.stakingKeeper.GetUBDQueueTimeSlice(ctx, t)
				return len(pairs) > 0, err
			},
			addSlot: func(ctx sdk.Context, t time.Time) error {
				return s.stakingKeeper.AddUBDQueuePendingSlot(ctx, t)
			},
			slots: func(ctx sdk.Context) ([]time.Time, error) {
				return s.stakingKeeper.GetUBDQueuePendingSlots(ctx)
			},
			dequeue: func(ctx sdk.Context) (delegators []string, err error) {
				pairs, err := s.stakingKeeper.DequeueAllMatureUBDQueue(ctx, now)
				for _, pair := range pairs {
					delegators = append(delegators, pair.DelegatorAddress)
				}
				return delegators, err
			},
		},
		{
			name:     "redelegation",
			key:      stakingtypes.RedelegationQueuePendingSlotsKey,
			entryKey: stakingtypes.GetRedelegationTimeKey,
			enqueue: func(ctx sdk.Context, t time.Time) error {
				return s.stakingKeeper.SetRedelegationQueueTimeSlice(ctx, t, []stakingtypes.DVVTriplet{{DelegatorAddress: delegator(t), ValidatorSrcAddress: "src", ValidatorDstAddress: "dst"}})
			},
			hasEntry: func(ctx sdk.Context, t time.Time) (bool, error) {
				triplets, err := s.stakingKeeper.GetRedelegationQueueTimeSlice(ctx, t)
				return len(triplets) > 0, err
			},
			addSlot: func(ctx sdk.Context, t time.Time) error {
				return s.stakingKeeper.AddRedelegationQueuePendingSlot(ctx, t)
			},
			slots: func(ctx sdk.Context) ([]time.Time, error) {
				return s.stakingKeeper.GetRedelegationQueuePendingSlots(ctx)
			},
			dequeue: func(ctx sdk.Context) (delegators []string, err error) {
				triplets, err := s.stakingKeeper.DequeueAllMatureRedelegationQueue(ctx, now)
				for _, triplet := range triplets {
					delegators = append(delegators, triplet.DelegatorAddress)
				}
				return delegators, err
			},
		},
	}
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	testCases := []struct {
		name         string
		pending      []time.Time
		stale        []time.Time // slots whose queue entry is already gone
		corrupt      []time.Time // slots whose queue entry cannot be decoded
		expMatured   []time.Time
		expRemaining []time.Time
		expWrites    int
		expErr       bool
	}{
		{name: "nothing matured", pending: []time.Time{future}, expRemaining: []time.Time{future}, expWrites: 0},
		{name: "one entry matured", pending: []time.Time{past, future}, expMatured: []time.Time{past}, expRemaining: []time.Time{future}, expWrites: 1},
		{name: "entry at the current time matured", pending: []time.Time{now, future}, expMatured: []time.Time{now}, expRemaining: []time.Time{future}, expWrites: 1},
		{name: "entries matured in time order", pending: []time.Time{now, future, past}, expMatured: []time.Time{past, now}, expRemaining: []time.Time{future}, expWrites: 1},
		{name: "stale slot dropped", pending: []time.Time{future}, stale: []time.Time{past}, expRemaining: []time.Time{future}, expWrites: 1},
		{name: "decode error leaves the store untouched", pending: []time.Time{past}, corrupt: []time.Time{now}, expRemaining: []time.Time{past, now}, expWrites: 0, expErr: true},
	}

	for _, q := range queues {
		for _, tc := range testCases {
			s.Run(q.name+"/"+tc.name, func() {
				s.SetupTest()
				// The header time is before every slot, so maturity must come from the currTime argument.
				headerTime := now.Add(-2 * time.Hour)
				ctx := s.ctx.WithBlockTime(headerTime).WithHeaderInfo(header.Info{Time: headerTime})
				for _, t := range tc.pending {
					s.Require().NoError(q.enqueue(ctx, t))
				}
				for _, t := range tc.stale {
					s.Require().NoError(q.addSlot(ctx, t))
				}
				for _, t := range tc.corrupt {
					s.Require().NoError(q.addSlot(ctx, t))
					ctx.KVStore(s.storeKey).Set(q.entryKey(t), []byte{0xff})
				}

				cms, ok := ctx.MultiStore().(storetypes.CommitMultiStore)
				s.Require().True(ok)
				cms.AddListeners([]storetypes.StoreKey{s.storeKey})
				cms.PopStateCache()
				matured, err := q.dequeue(ctx)
				if tc.expErr {
					s.Require().Error(err)
				} else {
					s.Require().NoError(err)
				}
				var expDelegators []string
				for _, t := range tc.expMatured {
					expDelegators = append(expDelegators, delegator(t))
				}
				s.Require().Equal(expDelegators, matured)
				writes := 0
				for _, pair := range cms.PopStateCache() {
					if bytes.Equal(pair.Key, q.key) {
						writes++
					}
				}
				s.Require().Equal(tc.expWrites, writes)

				remaining, err := q.slots(ctx)
				s.Require().NoError(err)
				s.Require().Equal(tc.expRemaining, remaining)

				for _, t := range tc.pending {
					has, err := q.hasEntry(ctx, t)
					s.Require().NoError(err)
					s.Require().Equal(tc.expErr || t.After(now), has, "queue entry at %s", t)
				}
			})
		}
	}
}

func (s *KeeperTestSuite) TestEndBlockRejectsMalformedPendingSlots() {
	queues := []struct {
		name     string
		key      []byte
		slotSize int
		endBlock func(ctx sdk.Context) error
	}{
		{
			name:     "validator",
			key:      stakingtypes.ValidatorQueuePendingSlotsKey,
			slotSize: 16,
			endBlock: func(ctx sdk.Context) error { return s.stakingKeeper.UnbondAllMatureValidators(ctx) },
		},
		{
			name:     "unbonding",
			key:      stakingtypes.UBDQueuePendingSlotsKey,
			slotSize: 8,
			endBlock: func(ctx sdk.Context) error {
				_, err := s.stakingKeeper.DequeueAllMatureUBDQueue(ctx, ctx.BlockTime())
				return err
			},
		},
		{
			name:     "redelegation",
			key:      stakingtypes.RedelegationQueuePendingSlotsKey,
			slotSize: 8,
			endBlock: func(ctx sdk.Context) error {
				_, err := s.stakingKeeper.DequeueAllMatureRedelegationQueue(ctx, ctx.BlockTime())
				return err
			},
		},
	}
	testCases := []struct {
		name   string
		value  func(slotSize int) []byte
		expErr error
	}{
		{
			name:   "shorter than the count",
			value:  func(int) []byte { return []byte{0, 0, 1} },
			expErr: stakingtypes.ErrPendingQueueSlotMissingCount,
		},
		{
			name: "count exceeds the stored slots",
			value: func(slotSize int) []byte {
				return append(binary.BigEndian.AppendUint32(nil, 2), make([]byte, slotSize)...)
			},
			expErr: stakingtypes.ErrPendingQueueSlotInsufficientCapacity,
		},
		{
			name:  "zero count",
			value: func(int) []byte { return binary.BigEndian.AppendUint32(nil, 0) },
		},
	}

	for _, q := range queues {
		for _, tc := range testCases {
			s.Run(q.name+"/"+tc.name, func() {
				s.SetupTest()
				s.ctx.KVStore(s.storeKey).Set(q.key, tc.value(q.slotSize))

				err := q.endBlock(s.ctx)
				if tc.expErr != nil {
					s.Require().ErrorIs(err, tc.expErr)
					return
				}
				s.Require().NoError(err)
			})
		}
	}
}

func (s *KeeperTestSuite) TestQueueTimesOutsideUnixNanoNeverMature() {
	queues := []struct {
		name     string
		enqueue  func(ctx sdk.Context, t time.Time) error
		endBlock func(ctx sdk.Context) error
		hasEntry func(ctx sdk.Context, t time.Time) bool
	}{
		{
			name: "validator",
			enqueue: func(ctx sdk.Context, t time.Time) error {
				// Not a known validator, so maturing it would fail end-block.
				return s.stakingKeeper.SetUnbondingValidatorsQueue(ctx, t, 1, []string{"unknown"})
			},
			endBlock: func(ctx sdk.Context) error { return s.stakingKeeper.UnbondAllMatureValidators(ctx) },
			hasEntry: func(ctx sdk.Context, t time.Time) bool {
				return ctx.KVStore(s.storeKey).Has(stakingtypes.GetValidatorQueueKey(t, 1))
			},
		},
		{
			name: "unbonding",
			enqueue: func(ctx sdk.Context, t time.Time) error {
				return s.stakingKeeper.SetUBDQueueTimeSlice(ctx, t, []stakingtypes.DVPair{{DelegatorAddress: "del", ValidatorAddress: "val"}})
			},
			endBlock: func(ctx sdk.Context) error {
				_, err := s.stakingKeeper.DequeueAllMatureUBDQueue(ctx, ctx.BlockTime())
				return err
			},
			hasEntry: func(ctx sdk.Context, t time.Time) bool {
				return ctx.KVStore(s.storeKey).Has(stakingtypes.GetUnbondingDelegationTimeKey(t))
			},
		},
		{
			name: "redelegation",
			enqueue: func(ctx sdk.Context, t time.Time) error {
				return s.stakingKeeper.SetRedelegationQueueTimeSlice(ctx, t, []stakingtypes.DVVTriplet{{DelegatorAddress: "del", ValidatorSrcAddress: "src", ValidatorDstAddress: "dst"}})
			},
			endBlock: func(ctx sdk.Context) error {
				_, err := s.stakingKeeper.DequeueAllMatureRedelegationQueue(ctx, ctx.BlockTime())
				return err
			},
			hasEntry: func(ctx sdk.Context, t time.Time) bool {
				return ctx.KVStore(s.storeKey).Has(stakingtypes.GetRedelegationTimeKey(t))
			},
		},
	}
	testCases := []struct {
		name       string
		completion time.Time
	}{
		{name: "after year 2262", completion: time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)},
		{name: "before year 1678", completion: time.Date(1600, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	for _, q := range queues {
		for _, tc := range testCases {
			s.Run(q.name+"/"+tc.name, func() {
				s.SetupTest()
				s.Require().NoError(q.enqueue(s.ctx, tc.completion))

				// The slot decodes to another time inside 1678-2262; the last block is past it either way.
				for _, blockTime := range []time.Time{tc.completion.Add(time.Hour), time.Date(2400, 1, 1, 0, 0, 0, 0, time.UTC)} {
					ctx := s.ctx.WithBlockTime(blockTime).WithHeaderInfo(header.Info{Time: blockTime}).WithBlockHeight(2)
					s.Require().NoError(q.endBlock(ctx))
					s.Require().True(q.hasEntry(ctx, tc.completion), "entry matured at block time %s", blockTime)
				}
			})
		}
	}
}
