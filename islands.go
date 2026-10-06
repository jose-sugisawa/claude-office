package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
)

// Island は islands.json の1行。係名が prefixes のどれかで始まる人がこの島に座る。
type Island struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Color    string   `json:"color"`              // 書いたまま（Warp のタブ色の名前か #RRGGBB）
	Prefixes []string `json:"prefixes,omitempty"` // 省略したら id
	Hex      string   `json:"hex,omitempty"`      // 画面で使う色（読むときに Color から決める。ファイルには書かない）
}

// warpColors は Warp のタブ色の名前を、オフィスで使う色にする。
var warpColors = map[string]string{
	"red": "#D9534F", "green": "#3FB36B", "yellow": "#E3B341",
	"blue": "#4C7FD6", "magenta": "#9B6FC2", "cyan": "#3FA7A2",
}

var (
	hexColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
	islandID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// LoadIslands は islands.json を読み、画面で使う色（Hex）と省略した prefixes を埋めて返す。
// 書き方の誤りは、どの行かを添えて返す。
func LoadIslands(b []byte) ([]Island, error) {
	var list []Island
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, fmt.Errorf("islands.json が JSON として読めません: %w", err)
	}
	seen := map[string]string{}
	for i := range list {
		is := &list[i]
		is.ID = strings.TrimSpace(is.ID)
		is.Name = strings.TrimSpace(is.Name)
		at := fmt.Sprintf("islands.json の %d 番目", i+1)
		if is.Name == "" {
			return nil, fmt.Errorf("%s：看板の名前（name）がありません", at)
		}
		if !islandID.MatchString(is.ID) {
			return nil, fmt.Errorf("%s（%s）：係名の頭（id）は英小文字・数字・- で書いてください", at, is.Name)
		}
		if c, ok := warpColors[strings.ToLower(is.Color)]; ok {
			is.Color, is.Hex = strings.ToLower(is.Color), c
		} else if hexColor.MatchString(is.Color) {
			is.Hex = is.Color
		} else {
			return nil, fmt.Errorf("%s（%s）：色（color）は red・green・yellow・blue・magenta・cyan か #RRGGBB で書いてください", at, is.Name)
		}
		if len(is.Prefixes) == 0 {
			is.Prefixes = []string{is.ID}
		}
		for j, p := range is.Prefixes {
			p = strings.ToLower(strings.TrimSpace(p))
			if !islandID.MatchString(p) {
				return nil, fmt.Errorf("%s（%s）：prefixes の %q は英小文字・数字・- で書いてください", at, is.Name, p)
			}
			if other, ok := seen[p]; ok {
				return nil, fmt.Errorf("%s（%s）：係名の頭 %q は「%s」の島と同じです", at, is.Name, p, other)
			}
			seen[p] = is.Name
			is.Prefixes[j] = p
		}
	}
	return list, nil
}

// SaveIslands は確かめたうえで islands.json を書き換える（1島1行。途中で止まっても壊れないよう、別名で書いてから置き換える）。
func SaveIslands(path string, list []Island) error {
	if _, err := LoadIslands(mustJSON(list)); err != nil {
		return err
	}
	var buf bytes.Buffer
	buf.WriteString("[\n")
	for i, is := range list {
		out := Island{ID: strings.TrimSpace(is.ID), Name: strings.TrimSpace(is.Name), Color: is.Color}
		if !(len(is.Prefixes) == 1 && is.Prefixes[0] == out.ID) {
			out.Prefixes = is.Prefixes
		}
		b, _ := json.Marshal(out)
		buf.WriteString("  ")
		buf.Write(b)
		if i < len(list)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("]\n")
	return writeFileAtomic(path, buf.Bytes())
}

// ensureIslands は、島の一覧がまだ無ければ見本を置く。
func ensureIslands(path string, example []byte) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return writeFileAtomic(path, example)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
