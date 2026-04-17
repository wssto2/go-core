package main

import (
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbtypes "github.com/wssto2/go-core/database/types"
	"go-core-example/internal/domain/product"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	coreauth "github.com/wssto2/go-core/auth"
	corei18n "github.com/wssto2/go-core/i18n"
)

type testViewer struct {
	id       int
	email    string
	policies []string
}

func (v testViewer) GetID() int            { return v.id }
func (v testViewer) GetEmail() string      { return v.email }
func (v testViewer) GetPolicies() []string { return v.policies }

func TestCatalogPageShellComposer_ComposePageShell_UsesTranslatedHeadFromLocale(t *testing.T) {
	t.Helper()

	composer := newCatalogPageShellComposer(loadConfig(), newTestTranslator(t), coreauth.TokenConfig{}, nil, nil, nil)

	shell, err := composer.ComposePageShell(context.Background(), pageShellRequest{
		Path:   "/products",
		Locale: "hr-HR,hr;q=0.9",
	})
	require.NoError(t, err)
	require.Equal(t, "Proizvodi | go-core example", shell.Head.Title)
	require.Equal(t, "Pregledajte serverski renderiran katalog proizvoda koji pokrecu go-core i Vue.", shell.Head.MetaDescription)
	require.Equal(t, "proizvodi, katalog proizvoda, go-core, SSR, Vue", shell.Head.MetaKeywords)
	require.Equal(t, "hr", shell.Bootstrap.Locale)
}

func TestCatalogPageShellComposer_ComposePageShell_BootstrapsCatalogProducts(t *testing.T) {
	t.Helper()

	composer := newCatalogPageShellComposer(loadConfig(), newTestTranslator(t), coreauth.TokenConfig{}, nil, func(ctx context.Context) ([]product.Product, error) {
		return []product.Product{
			{
				ID:          1,
				Name:        "Aurora Desk Lamp",
				SKU:         "AUR-LAMP-001",
				Description: dbtypes.NewNullString("Warm ambient light."),
				Price:       dbtypes.NewFloat(79),
				Stock:       4,
				Active:      dbtypes.NewBool(true),
			},
		}, nil
	}, nil)

	shell, err := composer.ComposePageShell(context.Background(), pageShellRequest{Path: "/"})
	require.NoError(t, err)
	require.Len(t, shell.Bootstrap.Catalog.Products, 1)
	require.Equal(t, 1, shell.Bootstrap.Catalog.Total)
	require.Equal(t, 1, shell.Bootstrap.Catalog.Active)
	require.Equal(t, 1, shell.Bootstrap.Catalog.LowStock)
	require.Equal(t, "Aurora Desk Lamp", shell.Bootstrap.Catalog.Products[0].Name)
}

func TestCatalogPageShellComposer_ComposePageShell_ComposesProductDetails(t *testing.T) {
	t.Helper()

	composer := newCatalogPageShellComposer(
		loadConfig(),
		newTestTranslator(t),
		coreauth.TokenConfig{},
		nil,
		func(ctx context.Context) ([]product.Product, error) {
			return []product.Product{
				{
					ID:          7,
					Name:        "Nimbus Travel Mug",
					SKU:         "NMB-MUG-002",
					Description: dbtypes.NewNullString("Insulated stainless steel mug."),
					Price:       dbtypes.NewFloat(24.50),
					Stock:       42,
					Active:      dbtypes.NewBool(true),
				},
				{
					ID:          9,
					Name:        "Atlas Notebook",
					SKU:         "ATL-NOTE-003",
					Description: dbtypes.NewNullString("Notebook."),
					Price:       dbtypes.NewFloat(18.90),
					Stock:       9,
					Active:      dbtypes.NewBool(true),
				},
			}, nil
		},
		func(ctx context.Context, id int) (product.Product, error) {
			require.Equal(t, 7, id)
			return product.Product{
				ID:          7,
				Name:        "Nimbus Travel Mug",
				SKU:         "NMB-MUG-002",
				Description: dbtypes.NewNullString("Insulated stainless steel mug."),
				Price:       dbtypes.NewFloat(24.50),
				Stock:       42,
				Active:      dbtypes.NewBool(true),
				ImageStatus: dbtypes.NewNullString(product.ImageStatusDone),
			}, nil
		},
	)

	shell, err := composer.ComposePageShell(context.Background(), pageShellRequest{
		Path:   "/products/7",
		Locale: "en-US",
	})
	require.NoError(t, err)
	require.NotNil(t, shell.Bootstrap.Product)
	require.Equal(t, "Nimbus Travel Mug", shell.Bootstrap.Product.Name)
	require.Equal(t, "Nimbus Travel Mug | go-core example", shell.Head.Title)
	require.Contains(t, shell.Head.MetaDescription, "SKU NMB-MUG-002")
	require.Empty(t, shell.Bootstrap.Catalog.Products)
	require.Equal(t, 2, shell.Bootstrap.Catalog.Total)
}

func TestCatalogPageShellComposer_ComposePageShell_ResolvesViewerFromBearerToken(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tokenCfg := coreauth.TokenConfig{
		SecretKey:     "change-me-to-32-bytes-minimum!!!",
		Issuer:        "go-core-example",
		TokenDuration: time.Hour,
	}
	token, err := coreauth.IssueToken(coreauth.Claims{
		UserID: 7,
		Email:  "admin",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject: "7",
		},
	}, tokenCfg)
	require.NoError(t, err)

	composer := newCatalogPageShellComposer(loadConfig(), newTestTranslator(t), tokenCfg, func(ctx context.Context, id string) (coreauth.Identifiable, error) {
		require.Equal(t, "7", id)
		return testViewer{
			id:       7,
			email:    "admin",
			policies: []string{"products:create"},
		}, nil
	}, nil, nil)

	shell, err := composer.ComposePageShell(context.Background(), pageShellRequest{
		Path:                "/",
		AuthorizationHeader: "Bearer " + token,
	})
	require.NoError(t, err)
	require.NotNil(t, shell.Bootstrap.Viewer)
	require.Equal(t, "admin", shell.Bootstrap.Viewer.Username)
}

func TestCatalogPageShellComposer_ComposePageShell_SurfacesViewerTokenErrors(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	tokenCfg := coreauth.TokenConfig{
		SecretKey:     "change-me-to-32-bytes-minimum!!!",
		Issuer:        "go-core-example",
		TokenDuration: time.Hour,
	}
	composer := newCatalogPageShellComposer(loadConfig(), newTestTranslator(t), tokenCfg, func(ctx context.Context, id string) (coreauth.Identifiable, error) {
		return testViewer{id: 1}, nil
	}, nil, nil)

	shell, err := composer.ComposePageShell(context.Background(), pageShellRequest{
		Path:                "/",
		AuthorizationHeader: "Bearer invalid-token",
	})
	require.NoError(t, err)
	require.Equal(t, "viewer token is invalid", shell.Bootstrap.ViewerError)
}

func TestSPAShellDataBuilder_Build_UsesPageShellComposer(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	composer := newCatalogPageShellComposer(loadConfig(), newTestTranslator(t), coreauth.TokenConfig{}, nil, nil, nil)
	builder := newSPAShellDataBuilder(composer)

	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Request = httptest.NewRequest("GET", "/?lang=hr", nil)

	shell, ok := builder.Build(ctx).(pageShellData)
	require.True(t, ok)
	require.Equal(t, "Katalog proizvoda | go-core example", shell.Head.Title)
	require.Equal(t, "hr", shell.Bootstrap.Locale)
}

func newTestTranslator(t *testing.T) *corei18n.Translator {
	t.Helper()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "en.json"), []byte(`{
  "seo": {
    "home": {
      "title": "Product Catalog | go-core example",
      "description": "A small product catalog showcasing go-core page composition, SSR-friendly metadata, and Vue hydration.",
      "keywords": "go-core, product catalog, SSR, Vue, Vite, server rendering"
    },
    "catalog": {
      "title": "Products | go-core example",
      "description": "Browse the server-rendered product catalog powered by go-core and hydrated by Vue.",
      "keywords": "products, product catalog, go-core, SSR, Vue"
    },
    "product": {
      "title": ":name | go-core example",
      "description": "Explore :name in the go-core demo catalog. SKU :sku with live server-composed product details.",
      "keywords": ":name, :sku, product details, go-core, SSR, catalog"
    },
    "productMissing": {
      "title": "Product not found | go-core example",
      "description": "The requested product could not be composed for the demo catalog page.",
      "keywords": "product not found, go-core, SSR, catalog"
    }
  },
  "catalog": {
    "title": "Server-rendered product catalog",
    "description": {
      "home": "The product grid below is composed in Go and bootstrapped into Vue through window.APP_STATE.",
      "index": "This route demonstrates page-specific metadata with the same server-composed catalog bootstrap.",
      "detail": "This product detail page is server-composed first, then hydrated by Vue with the same catalog bootstrap."
    },
    "error": {
      "unavailable": "Catalog query is unavailable.",
      "load": "Failed to load catalog products."
    }
  }
}`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "hr.json"), []byte(`{
  "seo": {
    "home": {
      "title": "Katalog proizvoda | go-core example",
      "description": "Mali katalog proizvoda koji prikazuje go-core kompoziciju stranice, SSR metadata i Vue hidrataciju.",
      "keywords": "go-core, katalog proizvoda, SSR, Vue, Vite, serverski prikaz"
    },
    "catalog": {
      "title": "Proizvodi | go-core example",
      "description": "Pregledajte serverski renderiran katalog proizvoda koji pokrecu go-core i Vue.",
      "keywords": "proizvodi, katalog proizvoda, go-core, SSR, Vue"
    },
    "product": {
      "title": ":name | go-core example",
      "description": "Pogledajte :name u go-core demo katalogu. SKU :sku sa serverski komponiranim detaljima proizvoda.",
      "keywords": ":name, :sku, detalji proizvoda, go-core, SSR, katalog"
    },
    "productMissing": {
      "title": "Proizvod nije pronaden | go-core example",
      "description": "Trazeni proizvod nije mogao biti komponiran za demo stranicu kataloga.",
      "keywords": "proizvod nije pronaden, go-core, SSR, katalog"
    }
  },
  "catalog": {
    "title": "Serverski renderiran katalog proizvoda",
    "description": {
      "home": "Donja mreza proizvoda komponira se u Go-u i bootstrapa u Vue kroz window.APP_STATE.",
      "index": "Ova ruta prikazuje metadata po stranici uz isti serverski komponiran bootstrap kataloga.",
      "detail": "Stranica detalja proizvoda prvo se komponira na serveru, a zatim hidrira u Vue uz isti bootstrap kataloga."
    },
    "error": {
      "unavailable": "Upit kataloga nije dostupan.",
      "load": "Ucavanje proizvoda nije uspjelo."
    }
  }
}`), 0o644))

	translator, err := corei18n.New(corei18n.Config{
		FallbackLang: "en",
		I18nDir:      dir,
	})
	require.NoError(t, err)
	return translator
}
