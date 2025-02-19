package keeper

import (
	"context"

	math "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/terra-money/alliance/x/alliance/types"
)

type RewardsKeeper interface{}

var _ RewardsKeeper = Keeper{}

// ClaimValidatorRewards claims the validator rewards (minus commission) from the distribution module
// This should be called everytime validator delegation changes (e.g. [un/re]delegation) to update the reward claim history
func (k Keeper) ClaimValidatorRewards(ctx context.Context, val types.AllianceValidator) (sdk.Coins, error) {
	moduleAddr := k.accountKeeper.GetModuleAddress(types.ModuleName)

	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	if err != nil {
		return nil, err
	}

	_, err = k.stakingKeeper.GetDelegation(ctx, moduleAddr, valAddr)
	if err != nil {
		return sdk.NewCoins(), nil
	}

	coins, err := k.distributionKeeper.WithdrawDelegationRewards(ctx, moduleAddr, valAddr)
	if err != nil || coins.IsZero() {
		return nil, err
	}
	err = k.AddAssetsToRewardPool(ctx, moduleAddr, val, coins)
	if err != nil {
		return nil, err
	}
	return coins, nil
}

// ClaimDelegationRewards claims delegation rewards and transfers to the delegator account
// This method updates the delegation so you will need to re-query an updated version from the database
func (k Keeper) ClaimDelegationRewards(
	ctx context.Context,
	delAddr sdk.AccAddress,
	val types.AllianceValidator,
	denom string,
) (sdk.Coins, error) {
	asset, found := k.GetAssetByDenom(ctx, denom)
	if !found {
		return nil, types.ErrUnknownAsset
	}

	sdkCtx := sdk.UnwrapSDKContext(ctx)
	if !asset.RewardsStarted(sdkCtx.BlockTime()) {
		return sdk.NewCoins(), nil
	}

	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	if err != nil {
		return nil, err
	}

	delegation, found := k.GetDelegation(ctx, delAddr, valAddr, denom)
	if !found {
		return sdk.Coins{}, stakingtypes.ErrNoDelegatorForAddress
	}

	_, err = k.ClaimValidatorRewards(ctx, val)
	if err != nil {
		return nil, err
	}

	coins, newIndices, err := k.CalculateDelegationRewards(ctx, delegation, val, asset)
	if err != nil {
		return nil, err
	}

	delegation.RewardHistory = newIndices
	delegation.LastRewardClaimHeight = uint64(sdkCtx.BlockHeight())
	if err = k.SetDelegation(ctx, delAddr, valAddr, denom, delegation); err != nil {
		return nil, err
	}

	if err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.RewardsPoolName, delAddr, coins); err != nil {
		return nil, err
	}

	_ = sdkCtx.EventManager().EmitTypedEvent(
		&types.ClaimAllianceRewardsEvent{
			AllianceSender: delAddr.String(),
			Validator:      val.OperatorAddress,
			Coins:          coins,
		},
	)

	return coins, nil
}

// CalculateDelegationRewards calculates the rewards that can be claimed for a delegation
// It takes past reward_rate changes into account by using the RewardRateChangeSnapshot entry
func (k Keeper) CalculateDelegationRewards(ctx context.Context, delegation types.Delegation, val types.AllianceValidator, asset types.AllianceAsset) (sdk.Coins, types.RewardHistories, error) {
	totalRewards := sdk.NewCoins()

	currentRewardHistory := types.NewRewardHistories(val.GlobalRewardHistory).GetIndexByAlliance(asset.Denom)
	delegationRewardHistories := types.NewRewardHistories(delegation.RewardHistory).GetIndexByAlliance(asset.Denom)
	valAddr, err := sdk.ValAddressFromBech32(val.OperatorAddress)
	if err != nil {
		return nil, nil, err
	}
	// If there are reward rate changes between last and current claim, sequentially claim with the help of the snapshots
	snapshotIter, err := k.IterateWeightChangeSnapshot(ctx, asset.Denom, valAddr, delegation.LastRewardClaimHeight)
	if err != nil {
		return nil, nil, err
	}
	for ; snapshotIter.Valid(); snapshotIter.Next() {
		var snapshot types.RewardWeightChangeSnapshot
		b := snapshotIter.Value()
		k.cdc.MustUnmarshal(b, &snapshot)
		var rewards sdk.Coins
		rewards, delegationRewardHistories = accumulateRewards(types.NewRewardHistories(snapshot.RewardHistories), delegationRewardHistories, asset, snapshot.PrevRewardWeight, delegation, val)
		totalRewards = totalRewards.Add(rewards...)
	}
	rewards, _ := accumulateRewards(currentRewardHistory, delegationRewardHistories, asset, asset.RewardWeight, delegation, val)
	totalRewards = totalRewards.Add(rewards...)
	return totalRewards, currentRewardHistory, nil
}

// accumulateRewards compares the latest reward history with the delegation's reward history
// It takes the difference and calculates how much can be claimed
func accumulateRewards(latestRewardHistories types.RewardHistories, rewardHistories types.RewardHistories, asset types.AllianceAsset, rewardWeight math.LegacyDec, delegation types.Delegation, validator types.AllianceValidator) (sdk.Coins, types.RewardHistories) {
	// Go through each reward denom and accumulate rewards
	var rewards sdk.Coins

	delegationTokens := math.LegacyNewDecFromInt(types.GetDelegationTokens(delegation, validator, asset).Amount)
	for _, history := range latestRewardHistories {
		rewardHistory, found := rewardHistories.GetIndexByDenom(history.Denom, history.Alliance)
		if !found {
			rewardHistory.Denom = history.Denom
			rewardHistory.Index = math.LegacyZeroDec()
			rewardHistory.Alliance = history.Alliance
		}
		if rewardHistory.Index.GTE(history.Index) {
			continue
		}
		var claimWeight math.LegacyDec
		// Handle legacy reward history that does not have a specific alliance
		if rewardHistory.Alliance == "" {
			claimWeight = delegationTokens.Mul(rewardWeight)
		} else {
			claimWeight = delegationTokens
		}
		totalClaimable := (history.Index.Sub(rewardHistory.Index)).Mul(claimWeight)
		rewardHistory.Index = history.Index
		rewards = rewards.Add(sdk.NewCoin(history.Denom, totalClaimable.TruncateInt()))
		if !found {
			rewardHistories = append(rewardHistories, *rewardHistory)
		}
	}
	return rewards, rewardHistories
}

// AddAssetsToRewardPool increments a reward history array. A reward history stores the average reward per token/reward_weight.
// To calculate the number of rewards claimable, take reward_history * alliance_token_amount * reward_weight
func (k Keeper) AddAssetsToRewardPool(ctx context.Context, from sdk.AccAddress, val types.AllianceValidator, coins sdk.Coins) error {
	rewardHistories := types.NewRewardHistories(val.GlobalRewardHistory)
	// We need some delegations before we can split rewards. Else rewards belong to no one and do nothing
	if len(val.TotalDelegatorShares) == 0 {
		return nil
	}
	alliances := k.GetAllAssets(ctx)

	totalStakedRewardWeight := math.LegacyZeroDec()
	// Map is used here only as a lookup table so it does not change the order of the results therefore it is consensus safe
	assetStakedRewardWeights := make(map[string]math.LegacyDec)
	for _, asset := range alliances {
		if shouldSkipRewardsToAsset(ctx, *asset, val) {
			continue
		}
		stakedRewardWeight := asset.RewardWeight.Mul(val.TotalTokensWithAsset(*asset)).QuoInt(asset.TotalTokens)
		assetStakedRewardWeights[asset.Denom] = stakedRewardWeight
		totalStakedRewardWeight = totalStakedRewardWeight.Add(stakedRewardWeight)
	}

	for _, asset := range alliances {
		if shouldSkipRewardsToAsset(ctx, *asset, val) {
			continue
		}
		normalizedWeight := assetStakedRewardWeights[asset.Denom].Quo(totalStakedRewardWeight)
		for _, c := range coins {
			rewardHistory, found := rewardHistories.GetIndexByDenom(c.Denom, asset.Denom)
			totalTokens := val.TotalTokensWithAsset(*asset)
			difference := math.LegacyNewDecFromInt(c.Amount).Mul(normalizedWeight).Quo(totalTokens)
			if !found {
				rewardHistories = append(rewardHistories, types.RewardHistory{
					Denom:    c.Denom,
					Alliance: asset.Denom,
					Index:    difference,
				})
			} else {
				rewardHistory.Index = rewardHistory.Index.Add(difference)
			}
		}
	}

	val.GlobalRewardHistory = rewardHistories
	if err := k.SetValidator(ctx, val); err != nil {
		return err
	}
	return k.bankKeeper.SendCoinsFromAccountToModule(ctx, from, types.RewardsPoolName, coins)
}

func shouldSkipRewardsToAsset(ctx context.Context, asset types.AllianceAsset, val types.AllianceValidator) bool {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return asset.TotalTokens.IsZero() || !asset.RewardsStarted(sdkCtx.BlockTime()) || val.TotalTokensWithAsset(asset).IsZero()
}
