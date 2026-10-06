#!/bin/sh
# claude-office を入れる（macOS・Linux）。clone したフォルダで ./install.sh を実行するだけ。
#
#   ./install.sh              入れる（実行ファイル → 自動起動 → ステータスライン → ブラウザで開く）
#   ./install.sh -y           聞かずに全部はいで進める
#   ./install.sh uninstall    外す（自動起動・ステータスライン・実行ファイル。島の一覧などは残す）
#
# 実行ファイルは ~/.local/bin/claude-office に置く（CLAUDE_OFFICE_BIN で変えられる）。
# Go があればこのフォルダからビルドし、なければ GitHub のリリースから取ってくる。
set -eu

REPO="jose-sugisawa/claude-office"
BIN_DIR="${CLAUDE_OFFICE_BIN:-$HOME/.local/bin}"
BIN="$BIN_DIR/claude-office"
ADDR="${CLAUDE_OFFICE_ADDR:-127.0.0.1:7777}"
HERE=$(cd "$(dirname "$0")" && pwd)
YES=0
CMD=install
for a in "$@"; do
  case "$a" in
    -y|--yes) YES=1 ;;
    uninstall) CMD=uninstall ;;
    -h|--help) sed -n '2,9p' "$0"; exit 0 ;;
    *) echo "知らない引数: $a" >&2; exit 2 ;;
  esac
done

say()  { printf '\033[1m%s\033[0m\n' "$*"; }
warn() { printf '\033[33m%s\033[0m\n' "$*" >&2; }
ask() { # ask "質問" → はいなら 0
  [ "$YES" = 1 ] && return 0
  [ -t 0 ] || return 1 # 聞けないとき（パイプや CI）は「いいえ」。全部はいで進めるなら -y
  printf '%s [Y/n] ' "$1"
  read -r ans || ans=
  case "$ans" in n|N|no|NO|いいえ) return 1 ;; *) return 0 ;; esac
}

if [ "$CMD" = uninstall ]; then
  if [ -x "$BIN" ]; then
    "$BIN" uninstall || true
    "$BIN" statusline uninstall || true
    rm -f "$BIN"
    say "外しました。島の一覧などは ~/.claude/office/ に残っています（要らなければ消してください）。"
  else
    warn "$BIN が見つかりません。CLAUDE_OFFICE_BIN で場所を指定してください。"
  fi
  exit 0
fi

# 1. 実行ファイル
mkdir -p "$BIN_DIR"
if command -v go >/dev/null 2>&1 && [ -f "$HERE/go.mod" ]; then
  say "1/4 ビルドしています（$(go version | awk '{print $3}')）"
  (cd "$HERE" && go build -trimpath -ldflags "-s -w" -o "$BIN" .)
else
  os=$(uname -s | tr '[:upper:]' '[:lower:]')
  arch=$(uname -m)
  case "$arch" in x86_64|amd64) arch=amd64 ;; arm64|aarch64) arch=arm64 ;; *) warn "この CPU（$arch）用の実行ファイルはありません。Go を入れてからやり直してください。"; exit 1 ;; esac
  file="claude-office_${os}_${arch}.tar.gz"
  base="https://github.com/$REPO/releases/latest/download"
  say "1/4 Go が無いので、リリースから取ってきます"
  echo "    $base/$file"
  tmp=$(mktemp -d)
  trap 'rm -rf "$tmp"' EXIT
  if ! curl -fsSL "$base/$file" -o "$tmp/c.tgz" || ! curl -fsSL "$base/checksums.txt" -o "$tmp/checksums.txt"; then
    warn "取ってこられませんでした。Go（https://go.dev/dl/）を入れてから、もう一度 ./install.sh を実行してください。"
    exit 1
  fi
  # リリースの checksums.txt と照らし合わせる（途中で書き換えられたものや壊れたものを入れない）
  want=$(awk -v f="$file" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt")
  if command -v sha256sum >/dev/null 2>&1; then got=$(sha256sum "$tmp/c.tgz" | awk '{print $1}')
  else got=$(shasum -a 256 "$tmp/c.tgz" | awk '{print $1}'); fi
  if [ -z "$want" ] || [ "$want" != "$got" ]; then
    warn "取ってきたファイルが checksums.txt と合いません（$file）。入れずに止めます。"
    exit 1
  fi
  tar -xzf "$tmp/c.tgz" -C "$tmp"
  install -m 755 "$tmp/claude-office" "$BIN"
fi
echo "    $BIN"

# 2. 自動起動（ログイン時に起動し、止まっても立ち上げ直す）
say "2/4 自動で起動するようにします"
if ! "$BIN" install -addr "$ADDR"; then
  warn "自動起動の登録はできませんでした。$BIN を手で実行すれば使えます。"
fi

# 3. ステータスライン（各セッションのいちばん下にコンテキストの使用率。オフィスのゲージと使用量もここから）
say "3/4 Claude Code のステータスラインに登録します"
if ask "    ~/.claude/settings.json に statusLine を足してよいですか（元の設定は settings.json.bak に残します）"; then
  if ! "$BIN" statusline install; then
    if ask "    別のステータスラインを置き換えますか"; then
      "$BIN" statusline install -force
    else
      warn "    登録しませんでした。コンテキストと使用量は出ませんが、ほかは使えます。"
    fi
  fi
else
  echo "    登録しませんでした。あとで $BIN statusline install で足せます。"
fi

# 4. PATH と、ブラウザで開く
case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    rc="$HOME/.profile"
    case "${SHELL:-}" in */zsh) rc="$HOME/.zshrc" ;; */bash) rc="$HOME/.bashrc" ;; esac
    line="export PATH=\"$BIN_DIR:\$PATH\""
    if grep -qsF "$line" "$rc"; then
      :
    elif ask "    $BIN_DIR が PATH にありません。$rc に足しますか（claude-office とだけ打てば動くようになります）"; then
      printf '\n# claude-office\n%s\n' "$line" >> "$rc"
      echo "    足しました。新しいターミナルから効きます。"
    fi
    ;;
esac

say "4/4 できました → http://$ADDR"
cat <<EOF

    次からは、係名を付けて Claude Code を起動してください：
      claude -n app@review     （「app」の島に、名札「review」で座る）
    島は画面の「＋ 島を作る」から作れます。
    外すときは ./install.sh uninstall です。

EOF
if command -v open >/dev/null 2>&1; then open "http://$ADDR" >/dev/null 2>&1 || true
elif command -v xdg-open >/dev/null 2>&1; then xdg-open "http://$ADDR" >/dev/null 2>&1 || true
fi
