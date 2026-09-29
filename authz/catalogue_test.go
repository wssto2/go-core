package authz_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wssto2/go-core/authz"
)

func TestDefineRejectsBadIdentifiers(t *testing.T) {
	tests := []struct {
		id string
		ok bool
	}{
		{"crm.lead:view", true},
		{"newvehicle:create", true},
		{"usedvehicle.evaluation.offer:decide", true},
		{"a1.b2:c3", true},
		{"crm.lead", false},             // no verb
		{":view", false},                // no namespace
		{"crm.lead:", false},            // empty verb
		{"crm.lead:view:all", false},    // two colons
		{"crm.lead:assign_user", false}, // separator in verb
		{"crm.lead_import:view", false}, // separator in segment
		{"crm.leadImport:view", false},  // upper case
		{"crm.lead-import:view", false}, // hyphen
		{"1crm.lead:view", false},       // starts with digit
		{"crm..lead:view", false},       // empty segment
		{"a.b.c.d:view", false},         // too deep
		{"crm.lead:View", false},        // upper case verb
		{"crm.lead :view", false},       // space
		{"", false},                     // empty
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			err := authz.NewCatalogue().Define(tt.id)
			if tt.ok {
				require.NoError(t, err)
				return
			}
			var ve *authz.ValidationError
			require.ErrorAs(t, err, &ve)
			assert.True(t, ve.Has(authz.ProblemInvalidIdentifier))
		})
	}
}

func TestDefineRejectsDuplicatesAndBadOptions(t *testing.T) {
	c := authz.NewCatalogue()
	require.NoError(t, c.Define("crm.lead:view"))

	var ve *authz.ValidationError
	require.ErrorAs(t, c.Define("crm.lead:view"), &ve)
	assert.True(t, ve.Has(authz.ProblemDuplicatePermission))

	for name, opt := range map[string]authz.DefineOption{
		"UnownedIsOwn without Ownable": authz.UnownedIsOwn(),
		"requires itself":              authz.Requires("crm.lead:edit"),
		"bad requires identifier":      authz.Requires("Bad"),
		"bad feature":                  authz.Feature("Two Words"),
		"bad attribute":                authz.Attributes("Kind"),
		"attribute twice":              authz.Attributes("kind", "kind"),
	} {
		t.Run(name, func(t *testing.T) {
			err := c.Define("crm.lead:edit", opt)
			require.ErrorAs(t, err, &ve)
			assert.True(t, ve.Has(authz.ProblemInconsistentMetadata), err)
		})
	}
	assert.Equal(t, 1, c.Len(), "rejected definitions leave no trace")
}

func TestValidateChecksRequires(t *testing.T) {
	t.Run("unknown target", func(t *testing.T) {
		c := authz.NewCatalogue()
		require.NoError(t, c.Define("crm.offer:update", authz.Requires("crm.offer:view")))
		var ve *authz.ValidationError
		require.ErrorAs(t, c.Validate(), &ve)
		assert.True(t, ve.Has(authz.ProblemUnknownRequirement))
	})
	t.Run("target defined later is fine", func(t *testing.T) {
		c := authz.NewCatalogue()
		require.NoError(t, c.Define("crm.offer:update", authz.Requires("crm.offer:view")))
		require.NoError(t, c.Define("crm.offer:view"))
		assert.NoError(t, c.Validate())
	})
	t.Run("cycle", func(t *testing.T) {
		c := authz.NewCatalogue()
		require.NoError(t, c.Define("a.b:one", authz.Requires("a.b:two")))
		require.NoError(t, c.Define("a.b:two", authz.Requires("a.b:one")))
		var ve *authz.ValidationError
		require.ErrorAs(t, c.Validate(), &ve)
		assert.True(t, ve.Has(authz.ProblemRequirementCycle))
	})
}

func TestCatalogueAccessorsAndFreeze(t *testing.T) {
	c := smallCatalogue(t)
	p, ok := c.Lookup("crm.lead:view")
	require.True(t, ok)
	assert.Equal(t, "crm", p.Module())
	assert.Equal(t, "crm.lead", p.Namespace())
	assert.Equal(t, "view", p.Verb())
	assert.True(t, p.Ownable())
	assert.True(t, p.UnownedIsOwn)
	assert.Equal(t, []string{"vehiclekind"}, c.AttributeKeys())
	assert.True(t, c.HasFeatures())
	assert.Equal(t, "crm.lead:view", c.All()[0].ID, "definition order is kept")

	// NewEngine freezes: nothing can be defined afterwards.
	w := newSmallWorld(t)
	err := w.Catalogue.Define("late.thing:view")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "frozen")
}

func TestMustDefinePanicsOnError(t *testing.T) {
	c := authz.NewCatalogue()
	assert.Panics(t, func() { c.MustDefine("Bad") })
	assert.NotPanics(t, func() { c.MustDefine("ok.thing:view") })
}
