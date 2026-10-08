package island

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	got, err := Load([]byte(`[{"id":"app","name":"APP","color":"green"},{"id":"web","name":"Web","color":"#4C7FD6","prefixes":["Web-App","web"]}]`))
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Hex != "#3FB36B" || got[0].Color != "green" || got[0].Prefixes[0] != "app" {
		t.Errorf("色の名前か prefixes の省略が効いていない: %+v", got[0])
	}
	if got[1].Prefixes[0] != "web-app" {
		t.Errorf("prefixes が小文字になっていない: %+v", got[1])
	}
	for _, bad := range []string{
		`[{"id":"x","name":"X","color":"pink"}]`,
		`[{"id":"x","color":"green"}]`,
		`[{"id":"x","name":"X","color":"green"},{"id":"x","name":"Y","color":"red"}]`,
		`{"id":"x"}`,
		`[{"id":"boss","name":"ボス","color":"green"}]`, // boss はボスの席に使う
		`[{"id":"x","name":"X","color":"green","prefixes":["x","boss"]}]`,
	} {
		if _, err := Load([]byte(bad)); err == nil {
			t.Errorf("誤りを見逃した: %s", bad)
		}
	}
	if _, err := Load(Example); err != nil {
		t.Errorf("同梱の見本が読めない: %v", err)
	}
}

func TestSaveKeepsColorNamesAndChecks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "islands.json")
	list := []Island{
		{ID: "app", Name: "APP", Color: "green", Prefixes: []string{"app"}, Hex: "#3FB36B"},
		{ID: "web-app", Name: "ウェブ", Color: "blue", Prefixes: []string{"web-app", "web"}},
	}
	if err := Save(path, list); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	want := "[\n  {\"id\":\"app\",\"name\":\"APP\",\"color\":\"green\"},\n  {\"id\":\"web-app\",\"name\":\"ウェブ\",\"color\":\"blue\",\"prefixes\":[\"web-app\",\"web\"]}\n]\n"
	if string(b) != want {
		t.Errorf("書いた中身が違う:\n%s", b)
	}
	// 頭が重なる・id が日本語、は書かずに断る
	for _, bad := range [][]Island{
		{{ID: "app", Name: "A", Color: "green"}, {ID: "x", Name: "B", Color: "red", Prefixes: []string{"app"}}},
		{{ID: "日本語", Name: "A", Color: "green"}},
	} {
		if err := Save(path, bad); err == nil {
			t.Errorf("誤りを見逃した: %+v", bad)
		}
	}
	if b2, _ := os.ReadFile(path); string(b2) != want {
		t.Errorf("断ったのにファイルが変わった")
	}
}
