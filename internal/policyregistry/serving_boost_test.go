package policyregistry_test

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
	"weave-os/router/internal/policyregistry"
	"weave-os/router/internal/subscriptions/entitlement"
)

func TestBoostRosterMovesAtomicallyWithDefault(t *testing.T) {
	store, original := cohortFixture(t)
	boost, ok := entitlement.ServingProfileFor(entitlement.PlanBoost)
	require.True(t, ok)
	original.Profiles[boost.Key] = original.DefaultPolicy
	controller := permissiveController(t, store)
	initial := cohortProposal(t, store, original, nil, policyregistry.ChangeFull, "")
	require.NoError(t, controller.ValidateProposal(context.Background(), initial))

	updated := original
	updated.Profiles = maps.Clone(original.Profiles)
	updated.DefaultPolicy = original.Profiles[profileKeyTwo]
	updated.Profiles[boost.Key] = updated.DefaultPolicy
	proposal := cohortProposal(t, store, updated, &initial.SelectionSet, policyregistry.ChangeRoster, "")
	require.NoError(t, controller.ValidateProposal(context.Background(), proposal))

	for name, mutate := range map[string]func(*policyregistry.SelectionSetV3){
		"stale boost":       func(set *policyregistry.SelectionSetV3) { set.Profiles[boost.Key] = original.DefaultPolicy },
		"unrelated profile": func(set *policyregistry.SelectionSetV3) { set.Profiles[profileKeyOne] = updated.DefaultPolicy },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := updated
			invalid.Profiles = maps.Clone(updated.Profiles)
			mutate(&invalid)
			proposal := cohortProposal(t, store, invalid, &initial.SelectionSet, policyregistry.ChangeRoster, "")
			require.Error(t, controller.ValidateProposal(context.Background(), proposal))
		})
	}
}

func TestBoostCannotActivateAnIndependentRoster(t *testing.T) {
	for _, scope := range []policyregistry.ChangeScope{policyregistry.ChangeFull, policyregistry.ChangeProfile, policyregistry.ChangeRollback} {
		t.Run(string(scope), func(t *testing.T) {
			store, original := cohortFixture(t)
			boost, ok := entitlement.ServingProfileFor(entitlement.PlanBoost)
			require.True(t, ok)
			original.Profiles[boost.Key] = original.DefaultPolicy
			initial := cohortProposal(t, store, original, nil, policyregistry.ChangeFull, "")
			invalid := original
			invalid.Profiles = maps.Clone(original.Profiles)
			invalid.Profiles[boost.Key] = original.Profiles[profileKeyTwo]
			profileKey := ""
			if scope == policyregistry.ChangeProfile {
				profileKey = boost.Key
			}
			proposal := cohortProposal(t, store, invalid, &initial.SelectionSet, scope, profileKey)
			require.ErrorContains(t, permissiveController(t, store).ValidateProposal(context.Background(), proposal), "Boost must use the default roster")
		})
	}
}

func TestBoostRosterMovesAtomicallyWithDefaultV2(t *testing.T) {
	store, _, original := controllerFixture(t)
	base := *store.object(t, policyregistry.ServingReleases, original.Default.Release).(*policyregistry.ServingRelease)
	boost, ok := entitlement.ServingProfileFor(entitlement.PlanBoost)
	require.True(t, ok)
	original.Profiles[boost.Key] = registerProfileFixture(t, store, original.Default, boost.Key, base.Policy)
	original.Profiles[profileKeyTwo] = registerProfileFixture(t, store, original.Default, profileKeyTwo, base.Policy)
	_, previous := foldSelectionSet(t, store, original)
	changedPolicy := publishRosterArm(t, store, base.Policy, alternateRosterArm)
	base.Policy = changedPolicy
	binding := store.object(t, policyregistry.ServingBindings, original.Default.Binding).(*policyregistry.DeploymentBinding)
	updated := original
	updated.Profiles = maps.Clone(original.Profiles)
	updated.Default = publishSelection(t, store, original.Default, base, binding.Router, binding.Classifier, nil)
	updated.Profiles[boost.Key] = registerProfileFixture(t, store, updated.Default, boost.Key, changedPolicy)
	_, next := foldSelectionSet(t, store, updated)
	proposal := policyregistry.DeploymentProposalV2{
		SchemaVersion: policyregistry.ServingProposalV2, Target: updated.Target, PreviousSelectionSet: &previous,
		SelectionSet: next, SourceCandidate: foldCandidate(t, store, updated.Default.Release), Scope: policyregistry.ChangeRoster,
		Actor: "test-operator", Reason: "shared Boost roster", RequestID: "synthetic-boost-release", CreatedAt: servingEpoch,
		Evidence: []policyregistry.ObjectRef{artifactRef("evidence")}, WithdrawActivations: []string{},
	}
	controller := permissiveController(t, store)
	require.NoError(t, controller.ValidateProposal(context.Background(), proposal))
	updated.Profiles[boost.Key] = original.Profiles[boost.Key]
	_, stale := foldSelectionSet(t, store, updated)
	proposal.SelectionSet = stale
	require.ErrorContains(t, controller.ValidateProposal(context.Background(), proposal), "Boost must use the default roster")
}
