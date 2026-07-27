# apricot

[![Test](https://github.com/ieee0824/apricot/actions/workflows/test.yml/badge.svg)](https://github.com/ieee0824/apricot/actions/workflows/test.yml)
[![codecov](https://codecov.io/gh/ieee0824/apricot/branch/main/graph/badge.svg)](https://codecov.io/gh/ieee0824/apricot)


[English](README.md)

[apple container](https://github.com/apple/container) で docker-compose みたいなことをしたい

## 目標
docker-compose.yaml をそのまま読み込める

## インストール

### Homebrew（推奨）

```bash
brew tap ieee0824/tap
brew install apricot
```

### go install

```bash
go install github.com/ieee0824/apricot/cmd/apricot@latest
```

### ソースからビルド

```bash
go build -o /usr/local/bin/apricot ./cmd/apricot/
```

## 使い方

`docker-compose.yaml` があるディレクトリで実行します。

### up

サービスを起動します。

```bash
apricot up                        # フォアグラウンドで起動
apricot up -d                     # バックグラウンドで起動
apricot up --scale web=3          # web を3インスタンス起動
apricot up --scale web=3 --scale db=2  # 複数サービスをスケール
apricot up -f path/to/docker-compose.yaml  # ファイルを指定
apricot up -p myproject                    # プロジェクト名を指定
```

`--scale` を指定したサービスのコンテナ名は `<project>-<service>-<index>` 形式になります（例: `myapp-web-1`, `myapp-web-2`）。

### build

`docker-compose.yaml` で定義されたイメージをビルドします。

```bash
apricot build           # 全サービスをビルド
apricot build web       # 特定サービスをビルド
```

### down

サービスを停止・削除します。

```bash
apricot down        # コンテナを停止・削除
apricot down -v     # ボリュームも削除
```

### ps

現在のプロジェクトのコンテナ一覧を表示します。

```bash
apricot ps
apricot ps -a       # 停止中のコンテナも表示
```

### logs

コンテナのログを表示します。

```bash
apricot logs              # 全サービスのログ
apricot logs web          # 特定サービスのログ
apricot logs -f web       # フォロー
```

### exec

実行中のサービスコンテナでコマンドを実行します。

```bash
apricot exec web sh             # sh を起動
apricot exec -it web bash       # インタラクティブ + TTY
apricot exec -u 1000 web whoami # ユーザー指定
apricot exec -w /app web pwd    # 作業ディレクトリ指定
```

| オプション | 説明 |
|---|---|
| `-t` | TTY を開く |
| `-i` | 標準入力を保持 |
| `-d` | デタッチして実行 |
| `-u <user>` | ユーザー指定 |
| `-w <dir>` | 作業ディレクトリ指定 |

## 共通オプション

| オプション | 説明 | デフォルト |
|---|---|---|
| `-f <file>` | docker-compose.yaml のパス | `docker-compose.yaml` |
| `-p <project>` | プロジェクト名 | カレントディレクトリ名 |

## docker-compose.yaml 対応フィールド

| フィールド | 対応 |
|---|---|
| `image` | ✅ |
| `build` | ✅ |
| `ports` | ✅ (短縮・ロング構文) |
| `volumes` | ✅ (短縮・ロング構文) |
| `environment` | ✅ |
| `env_file` | ✅ |
| `working_dir` | ✅ |
| `user` | ✅ |
| `entrypoint` | ✅ |
| `command` | ✅ |
| `platform` | ✅ |
| `networks` | ⚠️ (macOS 26+) |
| `labels` | ✅ |
| `cpus` | ✅ |
| `mem_limit` | ✅ |
| `stdin_open` | ✅ |
| `tty` | ✅ |
| `read_only` | ✅ |
| `tmpfs` | ✅ |
| `dns` | ✅ |
| `dns_search` | ✅ |
| `dns_opt` | ✅ |
| `init` | ✅ (`container run --init` にマッピング) |
| `ulimits` | ✅ (`container run --ulimit` にマッピング) |
| `cap_add` | ✅ (`container run --cap-add` にマッピング) |
| `cap_drop` | ✅ (`container run --cap-drop` にマッピング) |
| `depends_on` | ✅ (起動順序 + `condition: service_healthy`) |
| `healthcheck` | ✅ (`service_healthy` 待ちに使用) |
| `container_name` | ✅ |
| `restart` | ❌ (未対応) |
| `security_opt` | ❌ (Apple Container に相当機能なし) |

## 制限事項

- **networks**: デフォルト以外のネットワーク設定には macOS 26 以降が必要です（Apple Container ランタイムの制限）。macOS 26 未満では `networks` 設定は警告を出して自動的にスキップされます。
- **init**: `init: true` は `container run --init` として渡され、シグナル転送とゾンビプロセスの刈り取りを行う init プロセスが起動します（Apple Container v1.1.0 以降）。
- **ulimits**: shorthand（`nofile: 1024`）と long form（`nofile: {soft: 1024, hard: 2048}`）の両方に対応し、`container run --ulimit <type>=<soft>[:<hard>]` として渡されます（Apple Container v1.1.0 以降）。
- **cap_add / cap_drop**: `container run --cap-add` / `--cap-drop` として渡されます（Apple Container v0.12.0 以降）。capability 名はプレフィックス付き（`CAP_NET_RAW`）・なし（`NET_RAW`）のどちらでも動作し、`ALL` も使えます。
- **security_opt**: Apple Container CLI には `--security-opt` 相当のオプションがありません（VM 単位で分離するモデルのため seccomp / AppArmor プロファイルは適用対象外）。この設定は警告を出した上で無視されます。
- **healthcheck**: Apple Container はネイティブの healthcheck を持たないため、apricot が `test` コマンドを `container exec` でコンテナ内実行して判定します（`interval` / `timeout` / `retries` / `start_period` を尊重）。`depends_on: { x: { condition: service_healthy } }` を満たすために使われ、依存先が healthy になるまで `up` が待機します。`condition: service_completed_successfully` は未対応です。
- **未対応キー**: apricot が扱わないサービスキー（`deploy` / `restart` / `extends` / `profiles` など）は、compose ファイル読み込み時に警告を出します（無言で破棄しません）。
- **ビルドコンテキストのフィルタリング**: `container build` は `.dockerignore` で除外したファイルも含めてコンテキスト内の全ファイルを走査するため（[apple/container#2026](https://github.com/apple/container/issues/2026)）、`target/` や `node_modules` などの巨大な除外ツリーがあるとビルドが数分遅くなります。回避策として、コンテキストに `.dockerignore` がある場合、apricot は除外されなかったファイルだけの一時コピーを作ってそこからビルドします（APFS の clonefile を使うため高速でディスクも消費せず、ビルド後に削除されます）。`APRICOT_DISABLE_CONTEXT_FILTER=1` で無効化して元のコンテキストからビルドできます。
