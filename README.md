# apricot

[apple container](https://github.com/apple/container) で docker compose みたいなことをしたい

## 目標
docker-compose.yaml をそのまま読み込める

## インストール

```bash
go build -o /usr/local/bin/apricot ./cmd/apricot/
```

## 使い方

`docker-compose.yaml` があるディレクトリで実行します。

### up

サービスを起動します。

```bash
apricot up          # フォアグラウンドで起動
apricot up -d       # バックグラウンドで起動
apricot up -f path/to/docker-compose.yaml  # ファイルを指定
apricot up -p myproject                    # プロジェクト名を指定
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

## 共通オプション

| オプション | 説明 | デフォルト |
|---|---|---|
| `-f <file>` | docker-compose.yaml のパス | `docker-compose.yaml` |
| `-p <project>` | プロジェクト名 | カレントディレクトリ名 |

## docker-compose.yaml 対応フィールド

| フィールド | 対応 |
|---|---|
| `image` | ✅ |
| `ports` | ✅ |
| `volumes` | ✅ |
| `environment` | ✅ |
| `env_file` | ✅ |
| `working_dir` | ✅ |
| `user` | ✅ |
| `entrypoint` | ✅ |
| `command` | ✅ |
| `networks` | ✅ |
| `labels` | ✅ |
| `cpus` | ✅ |
| `mem_limit` | ✅ |
| `stdin_open` | ✅ |
| `tty` | ✅ |
| `read_only` | ✅ |
| `tmpfs` | ✅ |
| `dns` | ✅ |
| `depends_on` | ✅ (起動順序のみ) |
| `container_name` | ✅ |
| `restart` | ❌ (未対応) |
