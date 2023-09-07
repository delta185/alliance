package types

import (
	"fmt"
	"time"

	"golang.org/x/exp/slices"

	paramtypes "github.com/cosmos/cosmos-sdk/x/params/types"
)

var (
	RewardDelayTime       = []byte("RewardDelayTime")
	TakeRateClaimInterval = []byte("TakeRateClaimInterval")
	LastTakeRateClaimTime = []byte("LastTakeRateClaimTime")
	// ParamKeyTable deprecated - only used for migration
)

var _ paramtypes.ParamSet = (*Params)(nil)

// Deprecated: ParamKeyTable for auth module
func ParamKeyTable() paramtypes.KeyTable {
	return paramtypes.NewKeyTable().RegisterParamSet(&Params{})
}

func (p *Params) ParamSetPairs() paramtypes.ParamSetPairs {
	return paramtypes.ParamSetPairs{
		paramtypes.NewParamSetPair(RewardDelayTime, &p.RewardDelayTime, validatePositiveDuration),
		paramtypes.NewParamSetPair(TakeRateClaimInterval, &p.TakeRateClaimInterval, validatePositiveDuration),
		paramtypes.NewParamSetPair(LastTakeRateClaimTime, &p.LastTakeRateClaimTime, validateTime),
	}
}

func validatePositiveDuration(i interface{}) error {
	v, ok := i.(time.Duration)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	if v < 0 {
		return fmt.Errorf("duration must be positive: %d", v)
	}
	return nil
}

func validateTime(i interface{}) error {
	_, ok := i.(time.Time)
	if !ok {
		return fmt.Errorf("invalid parameter type: %T", i)
	}
	return nil
}

// NewParams creates a new Params instance
func NewParams() Params {
	return Params{
		RewardDelayTime:       time.Hour * 24 * 7,
		TakeRateClaimInterval: time.Minute * 5,
		LastTakeRateClaimTime: time.Time{},
	}
}

// DefaultParams returns a default set of parameters
func DefaultParams() Params {
	return NewParams()
}

type RewardHistories []RewardHistory

func NewRewardHistories(r []RewardHistory) RewardHistories {
	return r
}

func (r RewardHistories) GetIndexByDenom(denom string) (ri *RewardHistory, found bool) {
	idx := slices.IndexFunc(r, func(e RewardHistory) bool {
		return e.Denom == denom
	})
	if idx < 0 {
		return &RewardHistory{}, false
	}
	return &r[idx], true
}
