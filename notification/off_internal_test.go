//nolint:paralleltest,varnamelen // tests in the style of the package's others
package notification

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/env"
)

// NOTIF-OFF-001: a silenced category answers "off, enforced" on e-mail and push before the Enforce policy; every other
// category is the policy's answer unchanged. (That an enforced-off channel still writes the inbox entry is
// TestEnforcementDecidesWhoIsEmailed: the enforcer is asked per channel, in-app is not one.)
func TestWithOffSilencesListedCategoriesAheadOfTheEnforcer(t *testing.T) {
	silenced, other := Category("a.silenced"), Category("b.other")

	policy := func(_ context.Context, _ int, _ Kind, _ Channel) (bool, bool, error) {
		return true, true, nil // the policy forces everything on
	}
	enforce := withOff([]string{silenced.Code()}, policy)

	for _, ch := range []Channel{ChannelEmail, ChannelPush} {
		on, enforced, err := enforce(t.Context(), 1, silenced, ch)
		require.NoError(t, err)
		assert.False(t, on)
		assert.True(t, enforced)

		on, enforced, err = enforce(t.Context(), 1, other, ch)
		require.NoError(t, err)
		assert.True(t, on, "an unlisted category keeps the policy")
		assert.True(t, enforced)
	}
}

func TestWithOffNeedsNoPolicyAndIsNilWhenNothingIsSet(t *testing.T) {
	assert.Nil(t, withOff(nil, nil))

	enforce := withOff([]string{"a.silenced"}, nil)

	_, enforced, err := enforce(t.Context(), 1, Category("b.other"), ChannelEmail)
	require.NoError(t, err)
	assert.False(t, enforced)
}

func TestOffFromEnvReadsTheCommaListAndRefusesAnUnknownCode(t *testing.T) {
	vars := func(v string) *env.Reader {
		return env.New(func(string) (string, bool) { return v, true })
	}
	kinds := []Option{Category("a.one"), Category("b.two")}

	c := collect(append(kinds, OffFromEnv(vars(" a.one , ,b.two"))))
	require.Empty(t, c.problems)
	require.NotNil(t, c.enforce)

	c = collect(append(kinds, OffFromEnv(vars("a.one,a.typo"))))
	require.Len(t, c.problems, 1)
	assert.Contains(t, c.problems[0].What, `"a.typo"`)
	assert.Contains(t, c.problems[0].What, "a.one, b.two")

	c = collect(append(kinds, OffFromEnv(env.New(func(string) (string, bool) { return "", false }))))
	require.Empty(t, c.problems)
	assert.Nil(t, c.enforce, "unset silences nothing")
}
