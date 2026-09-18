package model

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testProvider(names ...string) *ModelProvider {
	models := make([]Model, 0, len(names))
	for i, name := range names {
		models = append(models, Model{Name: name, Index: i})
	}
	return &ModelProvider{models: models}
}

func TestReadModelsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.json")
	body := `[{"name":"a/one","base_url":"https://one.test/v1","auth_key":"ONE_KEY"},
		{"name":"b/two","base_url":"https://two.test/v1","auth_key":"TWO_KEY"}]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	models, err := readModelsFile(path)
	if err != nil {
		t.Fatalf("readModelsFile: %v", err)
	}
	// unexported fields would decode to "" here without ever erroring
	if len(models) != 2 || models[0].Name != "a/one" || models[1].BaseUrl != "https://two.test/v1" {
		t.Fatalf("got %+v", models)
	}
	if models[0].AuthKey != "ONE_KEY" || models[1].AuthKey != "TWO_KEY" {
		t.Fatalf("auth keys = %q, %q", models[0].AuthKey, models[1].AuthKey)
	}
}

func TestReadModelsFileRejectsUnusable(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"empty.json":   `[]`,
		"object.json":  `{}`,
		"partial.json": `[{"name":"a/one"}]`,
		"nokey.json":   `[{"name":"a/one","base_url":"https://one.test/v1"}]`,
		"garbage.json": `not json`,
	}
	for name, body := range cases {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := readModelsFile(path); err == nil {
			t.Errorf("%s: want an error so the environment fallback kicks in", name)
		}
	}
	if _, err := readModelsFile(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing file: want an error")
	}
}

func TestFailRotatesOffTheDeadModel(t *testing.T) {
	provider := testProvider("one", "two", "three")

	provider.Fail(provider.Model(), errors.New("connection refused"))
	if got := provider.Model().Name; got != "two" {
		t.Fatalf("after failing one, selected = %s, want two", got)
	}

	provider.Fail(provider.Model(), errors.New("connection refused"))
	if got := provider.Model().Name; got != "three" {
		t.Fatalf("after failing two, selected = %s, want three", got)
	}

	// everything is benched now, so the stalest gets paroled instead of the bot
	// hammering the model it just gave up on
	provider.Fail(provider.Model(), errors.New("connection refused"))
	if got := provider.Model().Name; got != "one" {
		t.Fatalf("all benched, selected = %s, want the stalest (one)", got)
	}
}

func TestJudgeSwapsOnceSlownessAddsUp(t *testing.T) {
	provider := testProvider("one", "two")

	// punish_threshold costs 2 a call, so it takes five before the swap
	for i := range 4 {
		provider.Judge(provider.Model(), punish_threshold)
		if got := provider.Model().Name; got != "one" {
			t.Fatalf("swapped after %d slow calls, too early", i+1)
		}
	}

	provider.Judge(provider.Model(), punish_threshold)
	if got := provider.Model().Name; got != "two" {
		t.Fatalf("selected = %s, want two once one crossed the skip score", got)
	}
}

func TestJudgeKillsOutrightPastTheKillThreshold(t *testing.T) {
	provider := testProvider("one", "two")
	provider.Judge(provider.Model(), kill_threshold)
	if got := provider.Model().Name; got != "two" {
		t.Fatalf("selected = %s, want two", got)
	}
	if score := provider.models[0].score; score < skip_score {
		t.Errorf("killed model score = %d, want it benched at %d", score, skip_score)
	}
}

func TestRewardIsFlooredAtZero(t *testing.T) {
	provider := testProvider("one")
	for range 10 {
		provider.Judge(provider.Model(), reward_threshold)
	}
	if score := provider.models[0].score; score != 0 {
		t.Fatalf("score = %d, want 0 so credit cannot be banked", score)
	}
}

func TestStaleVerdictDoesNotMoveTheRotation(t *testing.T) {
	provider := testProvider("one", "two", "three")

	// a request that started on "one" lands after something else rotated to "two"
	stale := provider.Model()
	provider.Rotate()

	provider.Judge(stale, kill_threshold)
	if got := provider.Model().Name; got != "two" {
		t.Fatalf("selected = %s, want two: a late verdict for one should not rotate again", got)
	}
	if score := provider.models[0].score; score < skip_score {
		t.Errorf("one's score = %d, want the verdict still counted against it", score)
	}
}

func TestParoleBringsABenchedModelBack(t *testing.T) {
	provider := testProvider("one", "two")

	provider.Fail(provider.Model(), errors.New("down"))
	if got := provider.Model().Name; got != "two" {
		t.Fatalf("selected = %s, want two", got)
	}

	// one has served its time; rotating off two should pick it back up
	provider.models[0].benched = time.Now().Add(-parole_period - time.Second)
	provider.Rotate()

	if got := provider.Model().Name; got != "one" {
		t.Fatalf("selected = %s, want one back after parole", got)
	}
	if score := provider.models[0].score; score != 0 {
		t.Errorf("paroled score = %d, want a fresh 0", score)
	}
}

func TestProviderResolvesKeysPerModel(t *testing.T) {
	t.Setenv("ONE_KEY", "secret-one")
	t.Setenv("TWO_KEY", "secret-two")

	provider, err := newModelProvider([]jsonModel{
		{Name: "a/one", BaseUrl: "https://one.test/v1", AuthKey: "ONE_KEY"},
		{Name: "b/two", BaseUrl: "https://two.test/v1", AuthKey: "TWO_KEY"},
	}, Chat)
	if err != nil {
		t.Fatalf("newModelProvider: %v", err)
	}
	if provider.Count() != 2 {
		t.Fatalf("count = %d, want 2", provider.Count())
	}
}

func TestProviderSkipsModelsWithNoKey(t *testing.T) {
	t.Setenv("ONE_KEY", "secret-one")

	provider, err := newModelProvider([]jsonModel{
		{Name: "a/one", BaseUrl: "https://one.test/v1", AuthKey: "MISSING_KEY"},
		{Name: "b/two", BaseUrl: "https://two.test/v1", AuthKey: "ONE_KEY"},
	}, Chat)
	if err != nil {
		t.Fatalf("newModelProvider: %v", err)
	}
	// the survivor has to be re-indexed, or Judge would attribute verdicts to
	// the wrong slot
	if provider.Count() != 1 || provider.models[0].Name != "b/two" || provider.models[0].Index != 0 {
		t.Fatalf("got %d models, first = %+v", provider.Count(), provider.models[0])
	}
}

// A service with no key of its own says so outright, so it is not confused
// with one whose variable is merely missing.
func TestProviderKeepsKeylessModels(t *testing.T) {
	t.Setenv("ONE_KEY", "secret-one")

	provider, err := newModelProvider([]jsonModel{
		{Name: "local/voice", BaseUrl: "/app/voices", AuthKey: noAuthKey},
		{Name: "a/one", BaseUrl: "https://one.test/v1", AuthKey: "ONE_KEY"},
		{Name: "b/two", BaseUrl: "https://two.test/v1", AuthKey: "MISSING_KEY"},
	}, Chat)
	if err != nil {
		t.Fatalf("newModelProvider: %v", err)
	}
	if provider.Count() != 2 {
		t.Fatalf("count = %d, want the keyless one and the keyed one", provider.Count())
	}
	if provider.models[0].Name != "local/voice" {
		t.Errorf("first model = %q", provider.models[0].Name)
	}
}

// NOP is the only spelling of it -- an unset NOP_KEY is still a missing key.
func TestProviderStillNeedsRealKeys(t *testing.T) {
	_, err := newModelProvider([]jsonModel{
		{Name: "a/one", BaseUrl: "https://one.test/v1", AuthKey: "NOP_KEY"},
	}, Chat)
	if err == nil {
		t.Fatal("want an error when the roster's only key is unset")
	}
}

func TestProviderFailsWhenNoKeysAreSet(t *testing.T) {
	_, err := newModelProvider([]jsonModel{
		{Name: "a/one", BaseUrl: "https://one.test/v1", AuthKey: "MISSING_KEY"},
	}, Chat)
	if err == nil {
		t.Fatal("want an error when nothing in the roster is usable")
	}
}
