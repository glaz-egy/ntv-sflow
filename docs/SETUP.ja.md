# セットアップ手順

Network Traffic Visualizer を手元で動かすための手順です。
**Docker を使用しない場合**（A）と**Docker を使用する場合**（B）の 2 通りを説明します。
Kubernetes で動かす場合は `docs/KUBERNETES.ja.md` を参照してください。

## 0. 現在の実装状況（最初にお読みください）

| コンポーネント | 状態 |
|---|---|
| Web UI（Globe View / Home Network View） | ✅ 動作します |
| モックデータ（決定的なトラフィックを生成） | ✅ 動作します |
| Go API サーバー（`cmd/api`、REST + WebSocket） | ✅ 動作します（モック / ライブ） |
| sFlow コレクター（`cmd/collector`） | ✅ 動作します |
| ライブモード（sFlow の実データを画面に表示） | ✅ 動作します |
| 履歴（タイムライン・過去の表示・再生） | ✅ 動作します（既定はメモリに 1 時間。ClickHouse を使うと永続化。E 章） |
| PostgreSQL（インベントリ・設定の保存） | ⏳ 未使用（インベントリは YAML のまま。D-060） |

画面右上の表示でデータの種類が分かります。`MOCK` はモックデータ、`SFLOW` は sFlow 機器からの実データです。
モックデータで動かす場合、sFlow 対応機器・データベース・GeoIP ファイルはいずれも不要です。
実データ（ライブモード）の手順は A-6 / B-5 を参照してください。
Go API を使う場合は、画面下部のタイムラインで過去の時間帯を表示・再生できます（E 章）。ブラウザ内モックでは履歴はありません。

### データの取得方法（2 種類）

| 方式 | 設定値 `NEXT_PUBLIC_DATA_MODE` | 仕組み | 必要なもの |
|---|---|---|---|
| ブラウザ内モック | `mock`（既定） | ブラウザの中でモックデータを生成 | Node.js のみ |
| Go API | `api` | Web UI が Go API から REST / WebSocket でデータを取得。API は「モック」または「ライブ（sFlow 実データ）」で動く | Node.js + Go（または Docker） |

どちらの方式でも、同じシード・シナリオなら **まったく同じデータ** が表示されます（両実装の一致はテストで保証しています）。
まず画面を見たいだけなら「ブラウザ内モック」で十分です。API の開発・動作確認をする場合は「Go API」を使います。

---

## A. Docker を使用しない場合

### A-1. 前提条件

| ソフトウェア | バージョン | 確認コマンド | 必要な場面 |
|---|---|---|---|
| Node.js | 20 以上（22 LTS 推奨。25 で動作確認済み） | `node -v` | 常に必要 |
| pnpm | 10.x | `pnpm -v` | 常に必要 |
| Go | 1.27 以上 | `go version` | Go API を使う場合、`pnpm test` を実行する場合 |
| ブラウザ | WebGL が有効な最新の Chrome / Edge / Firefox / Safari | — | 常に必要 |

pnpm が無い場合は、Node.js 同梱の Corepack で有効化できます。

```bash
corepack enable
```

```bash
corepack prepare pnpm@10.28.0 --activate
```

Go は macOS なら Homebrew でインストールできます（その他の OS は https://go.dev/dl/ を参照）。

```bash
brew install go
```

### A-2. 依存パッケージのインストール

リポジトリのルートディレクトリで実行します。

```bash
pnpm install
```

Go API を使う場合は、Go の依存モジュールも取得しておきます（省略しても初回ビルド時に自動で取得されます）。

```bash
go mod download
```

### A-3. ブラウザ内モックで起動する

```bash
pnpm dev
```

ブラウザで http://localhost:3000 を開くと、Globe View（`/globe`）が表示されます。
ソースコードを保存すると画面に自動で反映されます（ホットリロード）。
停止するには、ターミナルで `Ctrl + C` を押します。

ポート 3000 が使用中の場合は、別のポートを指定します。

```bash
pnpm --filter web dev --port 3001
```

#### モックの設定（任意）

**Next.js は `apps/web` ディレクトリの env ファイルを読み込みます**（ルートの `.env` は読み込まれません）。
`apps/web/.env.local` を作成して、次のように記述します。

```dotenv
NEXT_PUBLIC_MOCK_SCENARIO=heavy-download
NEXT_PUBLIC_MOCK_SEED=42
NEXT_PUBLIC_MOCK_SPEED=1
```

変更後は開発サーバーを再起動してください。
シナリオとシード値は、画面右上の **設定（⚙）メニュー** からも一時的に切り替えられます。

| シナリオ名 | 内容 |
|---|---|
| `default` | 標準的な家庭内トラフィック |
| `heavy-download` | pc-01 が単一の CDN から大容量ダウンロード |
| `internal-backup` | NAS へのバックアップが内部通信の大半を占める |
| `many-destinations` | 多数の宛先（グルーピングや Top-N の確認用） |
| `unknown-metadata` | 位置情報・デバイス情報・トポロジーが欠けている状態 |
| `stale-collector` | 開始 20 秒後にデータが途絶え、`Stale` 表示になる |

### A-4. Go API を使って起動する

ターミナルを 2 つ使います。

**ターミナル 1：API サーバーを起動**

```bash
pnpm dev:api
```

`configs/config.example.yaml` を読み込み、http://localhost:8080 で起動します。
起動確認：

```bash
curl http://localhost:8080/api/v1/status
```

**ターミナル 2：Web UI を API モードで起動**

`apps/web/.env.local` に次の 2 行を書きます（A-3 のモック設定は API モードでは使われません）。

```dotenv
NEXT_PUBLIC_DATA_MODE=api
NEXT_PUBLIC_API_BASE_URL=http://localhost:8080
```

```bash
pnpm dev
```

http://localhost:3000 を開きます。設定（⚙）メニューの「Data source」に API の URL が表示されていれば、API からデータを取得しています。

> ブラウザ内モックに戻すときは、`NEXT_PUBLIC_DATA_MODE=mock` にして（または行を削除して）開発サーバーを再起動します。

#### API サーバーの設定

設定は「既定値 → YAML ファイル → 環境変数」の順に上書きされます。
YAML のキー名を間違えるとエラーで起動しません（誤設定に気付けるようにするため）。

| 環境変数 | YAML のキー | 既定値 | 内容 |
|---|---|---|---|
| `APP_CONFIG` | — | （`pnpm dev:api` では `configs/config.example.yaml`） | 読み込む YAML ファイル |
| `API_LISTEN` | `api.listen` | `:8080` | 待ち受けアドレス |
| `API_CORS_ALLOWED_ORIGINS` | `api.cors_allowed_origins` | `http://localhost:3000` | API を呼べる Web UI の URL（カンマ区切り） |
| `MOCK_SCENARIO` | `mock.scenario` | `default` | モックのシナリオ |
| `MOCK_SEED` | `mock.seed` | `42` | 乱数シード |
| `MOCK_SPEED` | `mock.speed` | `1` | シミュレーション速度（倍率） |
| `NETWORK_INTERNAL_CIDRS` | `network.internal_cidrs` | RFC1918 + `fc00::/7` | 内部ネットワークとみなす CIDR（カンマ区切り） |

例：`internal-backup` シナリオで API を起動する

```bash
MOCK_SCENARIO=internal-backup pnpm dev:api
```

Web UI を 3000 番以外のポートで動かす場合は、そのURLを `API_CORS_ALLOWED_ORIGINS` に含めてください。

```bash
API_CORS_ALLOWED_ORIGINS=http://localhost:3001 pnpm dev:api
```

Go のバイナリを作る場合：

```bash
pnpm build:go
```

`bin/api`・`bin/collector`・`bin/sflow-gen` が作成されます。`./bin/api -config configs/config.example.yaml` のように起動できます。

### A-5. sFlow コレクターを動かす

sFlow を UDP 6343 番で受信し、解析・正規化した結果を 10 秒ごとにログへ出力します。
**現時点では画面には反映されません**（Milestone D で API に接続します）。

**ターミナル 1：コレクターを起動**

```bash
pnpm dev:collector
```

**ターミナル 2：テスト用の sFlow を送信（実機が無い場合）**

モックのトラフィックを本物の sFlow v5 データグラムに変換して送ります。

```bash
pnpm sflow-gen -target 127.0.0.1:6343 -scenario default
```

コレクターのログに、エクスポーターごとのサマリーが出力されます（`sampled_estimate_bps` はサンプリングからの推定値、`if1_counter_rx_bps` などはインターフェースカウンター由来の値です）。

メトリクス（Prometheus 形式）：

```bash
curl -s http://localhost:9102/metrics
```

`ntv_collector_datagrams_received_total` が増えていれば受信できています。

#### 実機（スイッチ・ルーター・サーバー）から送る場合

機器の sFlow 送信先（collector）を、コレクターを動かしているホストの IP アドレスと UDP 6343 番に設定してください。設定例は `docs/SFLOW.md` §14 にあります（hsflowd、Open vSwitch）。

- ホストのファイアウォールで UDP 6343 番の受信を許可してください。
- 送信元を限定する場合は `COLLECTOR_ALLOWED_SOURCES`（または `collector.allowed_sources`）に機器のアドレスを指定します。

```bash
COLLECTOR_ALLOWED_SOURCES=192.0.2.10,192.0.2.0/24 pnpm dev:collector
```

#### コレクターの設定

| 環境変数 | YAML のキー | 既定値 | 内容 |
|---|---|---|---|
| `COLLECTOR_LISTEN` | `collector.listen` | `0.0.0.0:6343` | sFlow の受信アドレス（UDP） |
| `COLLECTOR_METRICS_LISTEN` | `collector.metrics_listen` | `:9102` | `/metrics`・`/healthz` の公開アドレス（空で無効） |
| `COLLECTOR_ALLOWED_SOURCES` | `collector.allowed_sources` | （空：すべて許可） | 受け付ける送信元アドレス／CIDR（カンマ区切り） |
| `COLLECTOR_DEBUG_FLOWS_PER_SECOND` | `collector.debug_flows_per_second` | `0` | 1 以上で、正規化したサンプルを毎秒その件数まで JSON 出力（内部アドレスを含むため、デバッグ時のみ） |
| — | `collector.workers` / `queue_size` / `read_buffer_bytes` / `summary_interval` | `2` / `4096` / OS 既定 / `10s` | デコード並列数・キュー長・受信バッファ・サマリー間隔 |

### A-6. ライブモード（sFlow の実データを画面に表示）

API の中でコレクターが動き、受信した sFlow を 5 秒単位で集計して画面に表示します。
**A-5 の単体コレクターとは同じ UDP 6343 番を使うので、同時には起動しないでください。**

#### まずはデモで試す（実機なし）

モックと同じ構成のデモ用インベントリで、ジェネレーターの sFlow を表示します。

**ターミナル 1：ライブモードの API**

```bash
pnpm dev:live-demo
```

**ターミナル 2：テスト用 sFlow の送信**

```bash
pnpm sflow-gen -target 127.0.0.1:6343
```

**ターミナル 3：Web UI（API モード）**

`apps/web/.env.local` に A-4 と同じ 2 行（`NEXT_PUBLIC_DATA_MODE=api` と `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080`）を書いてから起動します。

```bash
pnpm dev
```

画面右上が `SFLOW` `Live` になれば成功です。ジェネレーターを止めると、約 15 秒後に `Stale` に変わります。

#### 実ネットワークで使う

1. **インベントリを作る（任意だが推奨）**：雛形をコピーして、自分の機器・エクスポーター・トポロジーを書きます。

   ```bash
   cp configs/inventory.example.yaml configs/inventory.local.yaml
   ```

   - `exporters`：sFlow を送る機器。`agent_address` はデータグラム内のエージェントアドレスです。`role: boundary` と `boundary_if_index`（WAN インターフェース）を指定すると、Download / Upload がインターフェースカウンターの実測値（`ctr`）になります。
   - `devices`：デバイスの ID・名前・種類・アドレス。**ID に IP アドレスは使えません**（IP は変わりうるため）。
   - インベントリが無くても動きます。その場合、内部の端末は「Unresolved device」（IP アドレスで表示）になります。
2. **GeoIP データベースを置く（任意）**：MaxMind の GeoLite2 City / ASN（無料アカウントでダウンロード可能、ライセンスに同意が必要）を `data/GeoLite2-City.mmdb` と `data/GeoLite2-ASN.mmdb` に置きます。無い場合、宛先は「Unknown location」に表示されます。
3. **API をライブモードで起動**：

   ```bash
   INVENTORY_FILE=configs/inventory.local.yaml pnpm dev:live
   ```

4. **機器の sFlow 送信先** を `<このホストの IP>:6343`（UDP）にします。設定例は `docs/SFLOW.md` §14 にあります。
5. Web UI は A-4 と同じく API モードで起動します。

#### ライブモードの設定

| 環境変数 | YAML のキー | 既定値 | 内容 |
|---|---|---|---|
| `APP_MODE` | `app.mode` | `mock` | `live` でライブモード |
| `INVENTORY_FILE` | `inventory_file` | （なし） | インベントリファイル |
| `GEOIP_CITY_DB` / `GEOIP_ASN_DB` | `geoip.city_db_path` / `asn_db_path` | `./data/GeoLite2-*.mmdb` | GeoIP データベース（無くても起動します） |
| `NETWORK_INTERNAL_CIDRS` | `network.internal_cidrs` | RFC1918 + `fc00::/7` | 内部とみなす CIDR。自組織のグローバルアドレスがあれば追加してください |
| — | `live.stale_after` | `15s` | この時間 sFlow が来ないと `Stale` 表示 |
| — | `app.aggregation_window` | `5s` | 集計窓の長さ |

コレクター関連の設定（`COLLECTOR_LISTEN`、`COLLECTOR_ALLOWED_SOURCES` など）は A-5 の表と同じです。ライブモードではメトリクスを API の `http://localhost:8080/metrics` で公開します。

### A-7. 本番ビルドでの起動（任意）

```bash
pnpm build
```

```bash
pnpm --filter web start
```

http://localhost:3000 で起動します。

### A-8. テスト・静的チェック

フロントエンドと Go の両方をテストします（Go が必要です）。

```bash
pnpm test
```

フロントエンドだけをテストする場合：

```bash
pnpm test:web
```

Go だけをテストする場合：

```bash
pnpm test:go
```

sFlow デコーダーの fuzz テスト（各 60 秒）：

```bash
pnpm fuzz
```

ESLint・`go vet`・`gofmt` のチェック：

```bash
pnpm lint
```

TypeScript の型チェック：

```bash
pnpm typecheck
```

#### API 仕様を変更したとき

`api/openapi.yaml` が API 仕様の正本です。変更したら TypeScript の型を再生成してください（再生成しないとテストが失敗します）。

```bash
pnpm gen:api
```

TypeScript 側のモック（`apps/web/src/lib/mock-backend`）を変更した場合は、Go との一致確認用データを更新してください。

```bash
pnpm update:golden
```

---

## B. Docker を使用する場合

### B-1. 前提条件

| ソフトウェア | バージョン | 確認コマンド |
|---|---|---|
| Docker Desktop または Docker Engine | 24 以上 | `docker --version` |
| Docker Compose（v2 プラグイン） | 2.x 以上 | `docker compose version` |

Docker Desktop を使う場合は、あらかじめアプリを起動しておいてください（`docker info` がエラーにならなければ準備完了です）。
Node.js・pnpm・Go をホストにインストールする必要はありません。

コマンドはすべてリポジトリのルートディレクトリで実行します。

### B-2. ブラウザ内モックで起動する（Web UI のみ）

```bash
docker compose -f deploy/docker-compose.dev.yml up --build -d web
```

初回はイメージのビルドに数分かかります。完了したら http://localhost:3000 を開いてください。

### B-3. Go API を使って起動する（Web UI + API）

```bash
WEB_DATA_MODE=api docker compose -f deploy/docker-compose.dev.yml up --build -d web api
```

| サービス | URL |
|---|---|
| Web UI | http://localhost:3000 |
| API | http://localhost:8080/api/v1/status |

> `WEB_DATA_MODE` は Web UI の **ビルド時** に埋め込まれます。モックと API を切り替えるときは、`--build` を付けて Web UI を再ビルドしてください。

> PostgreSQL / ClickHouse は現時点では使用しないため、起動する必要はありません（B-10 を参照）。

### B-4. sFlow コレクターを起動する

```bash
docker compose -f deploy/docker-compose.dev.yml up --build -d collector
```

UDP 6343 番で sFlow を受信し、http://localhost:9102/metrics でメトリクスを公開します。実機の送信先は、Docker を動かしているホストの IP アドレス・UDP 6343 番にしてください。

実機が無い場合は、デモ用のジェネレーターも一緒に起動できます（`--profile demo` が必要です）。

```bash
docker compose -f deploy/docker-compose.dev.yml --profile demo up --build -d collector sflow-gen
```

受信状況はログで確認できます。

```bash
docker compose -f deploy/docker-compose.dev.yml logs -f collector
```

> **現時点ではコレクターの結果は画面に反映されません**（Milestone D で API に接続します）。

### B-5. ライブモード（sFlow の実データを画面に表示）

API とコレクターを 1 つのコンテナ（`api-live`）で動かします。**B-4 の `collector` サービスと同じ UDP 6343 番を使うので、同時には起動しないでください。**

デモ（実機なし、デモ用インベントリ + ジェネレーター）：

```bash
WEB_DATA_MODE=api INVENTORY_FILE=/etc/ntv/inventory.demo.yaml SFLOW_TARGET=api-live:6343 docker compose -f deploy/docker-compose.dev.yml --profile live --profile demo up --build -d web api-live sflow-gen
```

実ネットワーク：

1. インベントリを `configs/` に置きます（例：`configs/inventory.local.yaml`）。コンテナ内では `/etc/ntv/custom/` として見えます。
2. GeoIP データベースを使う場合は `data/` に置きます（コンテナ内の `/data`）。
3. 起動します。

```bash
WEB_DATA_MODE=api INVENTORY_FILE=/etc/ntv/custom/inventory.local.yaml docker compose -f deploy/docker-compose.dev.yml --profile live up --build -d web api-live
```

機器の sFlow 送信先は `<Docker ホストの IP>:6343`（UDP）です。停止するときは `--profile live --profile demo` を付けて `down` してください。

### B-6. 状態とログの確認

```bash
docker compose -f deploy/docker-compose.dev.yml ps
```

`STATUS` が `Up ... (healthy)` になっていれば正常です。

```bash
docker compose -f deploy/docker-compose.dev.yml logs -f web api
```

### B-7. 停止

```bash
docker compose -f deploy/docker-compose.dev.yml --profile demo down
```

（`--profile demo` を付けると、デモ用ジェネレーターも含めて停止します。）

### B-8. 設定とポート変更（任意）

| 環境変数 | 既定値 | 内容 | 反映のタイミング |
|---|---|---|---|
| `WEB_DATA_MODE` | `mock` | `mock`：ブラウザ内モック／`api`：Go API から取得 | Web UI のビルド時 |
| `MOCK_SCENARIO` | `default` | モックのシナリオ（A-3 の表を参照） | `mock` 時は Web UI のビルド時、`api` 時は API の起動時 |
| `MOCK_SEED` | `42` | 乱数シード | 同上 |
| `MOCK_SPEED` | `1` | シミュレーション速度（倍率） | 同上 |
| `PUBLIC_HOST` | `localhost` | ブラウザから見たサーバーのホスト名・IP。別の PC から開く場合は必須 | Web UI のビルド時（API の CORS 設定にも反映） |
| `WEB_ORIGINS` | `http://<PUBLIC_HOST>:<WEB_PORT>` | API が受け付ける画面の URL（カンマ区切り。ホスト名と IP の両方で開く場合などに指定） | 起動時 |
| `WEB_PORT` | `3000` | Web UI のホスト側ポート | 起動時（API の CORS 設定にも自動で反映） |
| `API_PORT` | `8080` | API のホスト側ポート | 起動時（`api` モードでは Web UI の再ビルドも必要） |
| `COLLECTOR_PORT` | `6343` | コレクターのホスト側 UDP ポート | 起動時 |
| `COLLECTOR_METRICS_PORT` | `9102` | コレクターのメトリクスのホスト側ポート | 起動時 |
| `COLLECTOR_ALLOWED_SOURCES` | （空） | 受け付ける sFlow 送信元（カンマ区切り） | 起動時 |
| `HISTORY_BACKEND` | `memory` | 履歴の保存先：`memory`（API 再起動で消える）／`clickhouse`（`clickhouse` サービスも起動）／`none` | 起動時 |
| `INVENTORY_FILE` | （空） | `api-live` のインベントリ（コンテナ内のパス） | 起動時 |
| `GEOIP_CITY_FILE` / `GEOIP_ASN_FILE` | `GeoLite2-City.mmdb` / `GeoLite2-ASN.mmdb` | `./data` 内の GeoIP ファイル名（DB-IP Lite の場合は `dbip-city-lite.mmdb` / `dbip-asn-lite.mmdb`） | 起動時 |
| `SFLOW_TARGET` | `collector:6343` | `sflow-gen` の送信先（ライブモードのデモでは `api-live:6343`） | 起動時 |

例：API モード・`heavy-download` シナリオ・Web UI をポート 3100 で起動する場合

```bash
WEB_DATA_MODE=api MOCK_SCENARIO=heavy-download WEB_PORT=3100 docker compose -f deploy/docker-compose.dev.yml up --build -d web api
```

> API モードでは、シナリオの変更に Web UI の再ビルドは不要です（API を再起動するだけで反映されます）。

**サーバーで起動し、別の PC のブラウザから開く場合**は `PUBLIC_HOST` を必ず指定してください。
指定しないと、Web UI がブラウザ側の PC の `localhost:8080` に接続しようとして、データが表示されません。

```bash
WEB_DATA_MODE=api PUBLIC_HOST=server.example.lan docker compose -f deploy/docker-compose.dev.yml --profile live up --build -d web api-live
```

毎回指定する代わりに、値をファイルに書いておくこともできます。
リポジトリ直下に `docker.env` などのファイルを作成します（`.gitignore` に登録済みです）。

```dotenv
WEB_DATA_MODE=api
MOCK_SCENARIO=heavy-download
WEB_PORT=3100
```

`--env-file` でそのファイルを指定して起動します。

```bash
docker compose --env-file docker.env -f deploy/docker-compose.dev.yml up --build -d web api
```

> 注意：`--env-file` を付けない場合、Compose は compose ファイルと同じ `deploy/` ディレクトリの `.env` を読み込みます。リポジトリ直下の `.env` は自動では読み込まれません。

### B-9. Compose を使わずに docker コマンドだけで実行する場合

Go のイメージ（API・コレクター・ジェネレーター共通）：

```bash
docker build -f Dockerfile.backend -t ntv-go:dev .
```

API：

```bash
docker run --rm -p 8080:8080 -e MOCK_SCENARIO=default ntv-go:dev ntv-api
```

コレクター（UDP ポートは `/udp` を付けて公開します）：

```bash
docker run --rm -p 6343:6343/udp -p 9102:9102 ntv-go:dev ntv-collector
```

Web UI（API モード）：

```bash
docker build -t ntv-web:dev --build-arg NEXT_PUBLIC_DATA_MODE=api --build-arg NEXT_PUBLIC_API_BASE_URL=http://localhost:8080 .
```

```bash
docker run --rm -p 3000:3000 ntv-web:dev
```

### B-10. データベースコンテナについて（任意）

`deploy/docker-compose.dev.yml` の `clickhouse` サービスは、履歴を永続化するときに使います（E 章）。
PostgreSQL も定義されていますが、現在はアプリから使われません（D-060）。

---

## C. 動作確認チェックリスト

起動後、次の URL を開いて確認できます（ポートは環境に合わせて読み替えてください）。

### Web UI

| URL | 確認できること |
|---|---|
| http://localhost:3000/globe | 3D 地球儀上にアニメーションする通信の弧が表示される |
| http://localhost:3000/globe?dst=asn:13335 | Cloudflare が選択され、右側に通信元デバイスが表示される |
| http://localhost:3000/home?dst=asn:13335 | Home Network View で Cloudflare と通信しているデバイスが強調表示される |
| http://localhost:3000/globe?src=dev_pc01 | pc-01 の外部通信先だけが地球儀に表示される |
| http://localhost:3000/home?mode=hybrid | トポロジーの上に通信が重ねて表示される |

### API（Go API を使う場合）

| URL | 確認できること |
|---|---|
| http://localhost:8080/healthz | `ok` が返る |
| http://localhost:8080/api/v1/status | モード・シナリオ・最新ウィンドウの時刻 |
| http://localhost:8080/api/v1/globe?grouping=asn | ASN ごとの外部通信先 |
| http://localhost:8080/api/v1/home/traffic | デバイスと通信関係 |
| http://localhost:8080/api/v1/devices/dev_pc01 | pc-01 の詳細 |

API の仕様は `api/openapi.yaml` を参照してください。

### コレクター

| URL | 確認できること |
|---|---|
| http://localhost:9102/healthz | `ok` が返る |
| http://localhost:9102/metrics | 受信数・解析エラー・欠落推定などのメトリクス |

画面の見方：

- `≈` が付いた値は **sFlow サンプリングからの推定値** です（`est` タグ）。
- `ctr` タグの値は **インターフェースカウンター由来** の値です。
- 地球儀の弧は GeoIP の位置関係を示すもので、**実際の通信経路ではありません**。

---

## D. トラブルシューティング

| 症状 | 原因と対処 |
|---|---|
| `pnpm: command not found` | `corepack enable` を実行するか、pnpm をインストールしてください。 |
| `go: command not found` / `go.mod requires go >= 1.27` | Go 1.27 以上をインストールしてください。Go API を使わない場合は `pnpm test:web` でフロントエンドだけテストできます。 |
| `Port 3000 is already in use` / 8080 が使用中 | 別のポートを指定してください（A-3 / A-4 / B-8 を参照）。 |
| API モードで右上が `Connecting` や `Stale` のまま | API が起動しているか（`curl http://localhost:8080/healthz`）、`NEXT_PUBLIC_API_BASE_URL` が正しいか確認してください。API 停止中は `Stale` になり、API が復帰すると自動で再接続して `Live` に戻ります。 |
| ブラウザのコンソールに CORS エラー | Web UI の URL（例：`http://localhost:3001`）を API の `API_CORS_ALLOWED_ORIGINS` に追加して、API を再起動してください。 |
| API が `invalid configuration` で起動しない | ログに問題点がすべて表示されます。YAML のキー名の誤り、CIDR の書式、`app.mode: live`（未実装）などを確認してください。 |
| 地球儀が表示されない・真っ黒 | ブラウザの WebGL（ハードウェアアクセラレーション）が無効になっている可能性があります。ブラウザの設定で有効にしてください。 |
| 環境変数を変えてもシナリオが変わらない | ブラウザ内モックでは `NEXT_PUBLIC_*` はビルド時に反映されます。開発サーバーの再起動、または Docker なら `--build` を付けて再ビルドしてください。API モードでは API 側の `MOCK_SCENARIO` を変更して API を再起動します。 |
| Docker ビルド中にフォント取得で失敗する | ビルド時に Google Fonts をダウンロードします。インターネットに接続できる環境でビルドしてください（プロキシ環境ではプロキシ設定が必要です）。 |
| `Cannot connect to the Docker daemon` | Docker Desktop が起動していません。起動してから再実行してください。 |
| コレクターが何も受信しない（`datagrams_received_total` が 0 のまま） | 送信先 IP・ポート（UDP 6343）、ホストのファイアウォール、Docker では `-p 6343:6343/udp` の `/udp` を確認してください。`allowed_sources` を設定している場合は `ntv_collector_datagrams_rejected_total` も確認してください。 |
| `ntv_collector_datagrams_lost_estimate` や `datagrams_dropped_total` が増える | 受信が処理に追いついていません。`collector.read_buffer_bytes`（例：`8388608`）、`collector.workers`、`collector.queue_size` を増やしてください。ネットワーク経路で失われている場合もあります。 |
| ライブモードの API が `address already in use` で起動しない | UDP 6343 番を別のプロセス（単体コレクターや Docker の `collector` / `api-live`）が使っています。どちらかを止めるか、`COLLECTOR_LISTEN` で別ポートを指定してください。 |
| ライブモードで右上が「Waiting for sFlow」のまま | まだ 1 つも sFlow を受信していません。機器の送信先、ファイアウォール、`/metrics` の `ntv_collector_datagrams_received_total` を確認してください。 |
| 宛先がすべて「Unknown location」になる | GeoIP データベースがありません（起動ログに警告が出ます）。A-6 の手順 2 を参照してください。不明な位置を推測で埋めないのは仕様です。 |
| 端末が IP アドレスの「Unresolved device」で表示される | インベントリに登録されていない端末です。`devices` にアドレスを追加してください。 |
| インベントリを書き換えても反映されない | インベントリは API の起動時にだけ読み込まれます。`docker compose -f deploy/docker-compose.dev.yml --profile live restart api-live`（Docker 以外では API を再起動）で反映してください。書き間違いがあると API は起動に失敗するため、再起動後に `docker compose ... logs api-live` で `"msg":"live mode"` と `devices` の件数を確認します（例：`topology` に存在しないデバイス ID を書くと `endpoints must be device ids` で失敗します）。 |
| Download / Upload に `ctr` ではなく `est` が付く | WAN インターフェースのカウンターが届いていません。インベントリの該当エクスポーターに `role: boundary` と正しい `boundary_if_index` を設定し、機器側でカウンターサンプリング（polling）を有効にしてください。 |
| `ntv_collector_decoder_errors_total` が増える | sFlow v5 以外（v2/v4 や NetFlow など）が届いている可能性があります。機器の設定を確認してください。 |
| 右上に `Stale` と表示される（モックモード） | `stale-collector` シナリオでは仕様どおりの表示です。それ以外では、ブラウザのタブがバックグラウンドで更新が止まっていた可能性があります。再読み込みしてください。 |

---

## E. 履歴（タイムライン・過去の表示・再生）

Go API を使う構成（`NEXT_PUBLIC_DATA_MODE=api`）では、画面の下部にタイムラインが表示されます。
API が受信した集計前のデータを 1 秒単位で保存し、指定した時間帯の Globe / Home / インスペクター / フロー検索を、ライブと同じ処理で組み立て直して表示します（D-059）。

### E-1. 使い方

| 操作 | 結果 |
|---|---|
| タイムラインをクリック | その時刻を中心に、選択中の長さ（既定 5 分）の時間帯を表示 |
| タイムラインをドラッグ | ドラッグした範囲をそのまま表示 |
| `‹` / `›`、またはタイムラインにフォーカスして ← / → | 1 区間ずつ前後に移動 |
| `Replay` と速度（0.5×〜4×） | 時間帯を少しずつ進めて再生（1× = 1 秒に 1 秒） |
| `Live` | ライブ表示に戻る |
| 左の範囲（15 min〜7 d） | タイムラインに表示する期間 |
| 右の長さ（1 min〜6 h） | 表示する時間帯の長さ |

過去の時間帯を表示している間は、右上が `History`、下部が `History window …` と表示されます。
URL に `?at=…&span=…` が付くので、そのまま共有・ブックマークでき、Globe と Home を切り替えても同じ時間帯が保たれます。

値の意味：
- 過去の値は、選んだ時間帯の**平均**です（≈ はサンプリングによる推定値）。WAN のカウンター（`CTR`）が時間帯の 8 割以上をカバーしていれば、Download / Upload はカウンターの値になります。
- 記録が始まる前・最新データより後の部分は、平均の計算に含めません。途中で記録が止まっていた時間は「通信なし」として扱われます。タイムラインでは空白として表示されます。
- 端末名などのインベントリ情報は**現在のもの**で表示されます。

### E-2. 保存先

| `HISTORY_BACKEND` | 保存期間 | 特徴 |
|---|---|---|
| `memory`（既定） | `history.memory_retention`（既定 1 時間） | 追加の準備は不要です。API を再起動すると消えます |
| `clickhouse` | 1 秒単位 7 日、1 分単位 90 日、1 時間単位 365 日（`history.*` で変更可） | 再起動しても残ります。長い期間は自動的に粗い単位で表示されます |
| `none` | — | 履歴は無効です（タイムラインは表示されません） |

### E-3. ClickHouse を使う（Docker）

```bash
HISTORY_BACKEND=clickhouse docker compose -f deploy/docker-compose.dev.yml --profile live up --build -d clickhouse api-live web
```

モックモードの場合は、`api-live` を `api` に置き換えます。毎回指定する代わりに、`deploy/.env` に `HISTORY_BACKEND=clickhouse` と書いておくこともできます。
API は起動時に ClickHouse の準備ができるまで待ち（最大 1 分）、テーブルの作成と保存期間の設定を自動で行います。

### E-4. ClickHouse を使う（Docker なし）

ClickHouse を別途用意し、HTTP インターフェース（ポート 8123）の URL を指定して API を起動します。

```bash
HISTORY_BACKEND=clickhouse CLICKHOUSE_DSN=http://user:password@localhost:8123/ntv pnpm dev:live
```

パスワードは設定ファイルに書かず、環境変数で渡してください。

### E-5. 確認

```bash
curl -s http://localhost:8080/api/v1/status
```

`history` に `backend` と記録範囲（`earliest` / `latest`）が出ていれば有効です。`GET /metrics` の `ntv_history_rows_written_total` が増えていれば保存されています。`ntv_history_rows_dropped_total` が増える場合は、保存が追いついていません（ライブ表示には影響しません）。

計画と制約は `docs/IMPLEMENTATION_PLAN.md`、設計は `docs/DECISIONS.md` の D-059 を参照してください。
