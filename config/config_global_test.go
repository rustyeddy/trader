package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/rustyeddy/trader/review"
)

func writeYAML(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0644))
}

func TestLoadGlobalConfig_EmptyDirs(t *testing.T) {
	cfg, err := loadGlobalConfig([]string{t.TempDir()}, "")
	require.NoError(t, err)
	assert.Equal(t, &GlobalConfig{}, cfg)
}

func TestLoadGlobalConfig_MissingDirSkipped(t *testing.T) {
	cfg, err := loadGlobalConfig([]string{"/nonexistent/trader"}, "")
	require.NoError(t, err)
	assert.Equal(t, &GlobalConfig{}, cfg)
}

func TestLoadGlobalConfig_SingleFile(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `
log:
  level: info
  format: json
data:
  dir: /srv/data
`)
	cfg, err := loadGlobalConfig([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, "info", cfg.Log.Level)
	assert.Equal(t, "json", cfg.Log.Format)
	assert.Equal(t, "/srv/data", cfg.Data.Dir)
}

func TestLoadGlobalConfig_MergeOrder(t *testing.T) {
	// /etc dir sets level=warn; user dir overrides to info.
	etcDir := t.TempDir()
	userDir := t.TempDir()
	writeYAML(t, etcDir, "base.yml", `
log:
  level: warn
  format: text
oanda:
  env: practice
`)
	writeYAML(t, userDir, "override.yml", `
log:
  level: info
oanda:
  token: mytoken
`)
	cfg, err := loadGlobalConfig([]string{etcDir, userDir}, "")
	require.NoError(t, err)
	assert.Equal(t, "info", cfg.Log.Level)  // user overrode etc
	assert.Equal(t, "text", cfg.Log.Format) // etc value kept (user didn't set it)
	assert.Equal(t, "practice", cfg.OANDA.Env)
	assert.Equal(t, "mytoken", cfg.OANDA.Token)
}

func TestLoadGlobalConfig_AlphabeticWithinDir(t *testing.T) {
	// a.yml sets level=debug; b.yml overrides to info — b wins.
	dir := t.TempDir()
	writeYAML(t, dir, "a.yml", `log: {level: debug}`)
	writeYAML(t, dir, "b.yml", `log: {level: info}`)
	cfg, err := loadGlobalConfig([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, "info", cfg.Log.Level)
}

func TestLoadGlobalConfig_ExplicitPathOverrides(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "base.yml", `log: {level: warn}`)

	explicit := filepath.Join(t.TempDir(), "explicit.yml")
	require.NoError(t, os.WriteFile(explicit, []byte(`log: {level: debug}`), 0644))

	cfg, err := loadGlobalConfig([]string{dir}, explicit)
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level) // explicit wins over dir
}

func TestLoadGlobalConfig_ExplicitPathMissing(t *testing.T) {
	_, err := loadGlobalConfig(nil, "/nonexistent/explicit.yml")
	assert.Error(t, err)
}

func TestLoadGlobalConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "bad.yml", `log: {level: [not: a: string]}`)
	_, err := loadGlobalConfig([]string{dir}, "")
	assert.Error(t, err)
}

func TestLoadGlobalConfig_OANDAFields(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "oanda.yml", `
oanda:
  token: tok123
  account_id: acc456
  env: live
`)
	cfg, err := loadGlobalConfig([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, "tok123", cfg.OANDA.Token)
	assert.Equal(t, "acc456", cfg.OANDA.AccountID)
	assert.Equal(t, "live", cfg.OANDA.Env)
}

func TestLoadGlobalConfig_DBField(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "db.yml", `db: /var/lib/trader/journal.db`)
	cfg, err := loadGlobalConfig([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, "/var/lib/trader/journal.db", cfg.DB)
}

func TestMergeGlobalConfig_EmptyDoesNotOverwrite(t *testing.T) {
	dst := &GlobalConfig{}
	dst.Log.Level = "info"
	dst.OANDA.Token = "existing"

	mergeGlobalConfig(dst, &GlobalConfig{}) // all empty src
	assert.Equal(t, "info", dst.Log.Level)
	assert.Equal(t, "existing", dst.OANDA.Token)
}

func TestLoadGlobalConfig_ReviewFields(t *testing.T) {
	dir := t.TempDir()
	writeYAML(t, dir, "review.yml", `
review:
  h4_adx_floor: 15.0
  h4_min_ema_sep: 0.2
  week_used_caution: 0.80
`)
	cfg, err := loadGlobalConfig([]string{dir}, "")
	require.NoError(t, err)
	assert.Equal(t, 15.0, cfg.Review.H4ADXFloor)
	assert.Equal(t, 0.2, cfg.Review.H4MinEMASep)
	assert.Equal(t, 0.80, cfg.Review.WeekUsedCaution)
}

func TestGlobalReviewConfig_ToThresholds_FallsBackToDefaults(t *testing.T) {
	var cfg GlobalReviewConfig
	assert.Equal(t, review.DefaultThresholds(), cfg.ToThresholds())
}

func TestGlobalReviewConfig_ToThresholds_OverridesOnlyConfiguredFields(t *testing.T) {
	cfg := GlobalReviewConfig{H4ADXFloor: 15.0}
	th := cfg.ToThresholds()

	assert.Equal(t, 15.0, th.H4ADXFloor)
	assert.Equal(t, review.DefaultThresholds().HotD1ADXFloor, th.HotD1ADXFloor)
}
