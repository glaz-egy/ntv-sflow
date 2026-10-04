# Kubernetes へのデプロイ手順

Network Traffic Visualizer を Kubernetes クラスタで動かす手順です。
マニフェストは `deploy/k8s/` にあり、Kustomize（`kubectl apply -k`）で適用します。
ローカル（Docker / Compose）での起動手順は `docs/SETUP.ja.md` を参照してください。

## 0. 構成と前提

### 0-1. デプロイ構成（3 種類）

| ディレクトリ | 内容 | 用途 |
|---|---|---|
| `deploy/k8s/base` | Web UI + Go API（**モックモード**）+ Ingress | まず動かしてみる。sFlow 機器は不要 |
| `deploy/k8s/overlays/live` | base + **ライブモード**（API に sFlow コレクターを内蔵）+ UDP/6343 の LoadBalancer | 実機の sFlow を可視化する本番構成 |
| `deploy/k8s/overlays/demo` | live + `sflow-gen`（モック由来の sFlow をクラスタ内で送信） | 実機なしでライブモードの経路を確認する |
| `deploy/k8s/components/geoip` | GeoIP データベース用 PVC のマウント（任意） | live に追加して国・都市・ASN を表示する |

```text
                ┌──────────── Ingress (ntv.home.arpa) ────────────┐
ブラウザ ──HTTP─▶│  /api/*  → Service ntv-api:8080  (REST + WebSocket) │
                │  /*      → Service ntv-web:3000  (Next.js)          │
                └──────────────────────────────────────────────────┘
sFlow 機器 ──UDP/6343─▶ Service ntv-sflow (LoadBalancer) ─▶ Pod ntv-api（コレクター内蔵）
```

### 0-2. 知っておくべき制約

| 項目 | 内容 | 理由 |
|---|---|---|
| API は **1 レプリカ固定** | `replicas: 1`、`strategy: Recreate` | ライブ集計と WebSocket クライアントはプロセス内メモリに保持しているため（D-053）。複数にすると Pod ごとに異なるデータになります |
| Web UI の接続先は**ビルド時に決まる** | `NEXT_PUBLIC_API_BASE_URL` はイメージに埋め込まれます | Next.js の `NEXT_PUBLIC_*` の仕様。ホスト名を変えたらイメージの再ビルドが必要です |
| 認証なし | UI / API にログイン機能はまだありません（D-019） | 信頼できるネットワーク内だけで公開してください（`docs/SECURITY.md`） |
| 履歴は保存しない | 直近の集計ウィンドウのみ表示 | PostgreSQL / ClickHouse は未実装（Milestone E）。このマニフェストには含めていません |
| sFlow の送信元 IP | `externalTrafficPolicy: Local` で保持 | `collector.allowed_sources` が UDP の送信元 IP で判定するため（D-049） |

### 0-3. 必要なもの

| もの | 備考 |
|---|---|
| Kubernetes 1.27 以上 | k3s / kind / minikube / Docker Desktop / マネージド K8s のいずれでも可 |
| `kubectl`（Kustomize 内蔵） | `kubectl version --client` で確認 |
| Docker（イメージのビルド用） | ビルド済みイメージをレジストリから取得する場合は不要 |
| Ingress コントローラー | Traefik（k3s 標準）など。デフォルトの IngressClass があればそれを使います |
| LoadBalancer の実装（live のみ） | クラウドの LB、MetalLB、k3s の ServiceLB など |
| DNS / hosts の設定 | 例のホスト名 `ntv.home.arpa` をブラウザから引けるようにします |

---

## 1. ホスト名を決める

ブラウザで開く URL を 1 つ決めます（以降の例では `http://ntv.home.arpa`）。
UI と API は**同じホスト**で公開します（`/api/*` だけ API に振り分け）。同じ値を次の **3 か所** に設定してください。

| # | 場所 | 設定する値 |
|---|---|---|
| 1 | `deploy/k8s/base/ingress.yaml` の `rules[].host` | `ntv.home.arpa` |
| 2 | `deploy/k8s/base/config.yaml` の `api.cors_allowed_origins` | `http://ntv.home.arpa` |
| 3 | Web イメージのビルド引数 `NEXT_PUBLIC_API_BASE_URL`（§2） | `http://ntv.home.arpa` |

> 2 は WebSocket の接続元チェックにも使われます（D-041）。ここがずれると、画面は表示されても `Connecting` のまま更新されません。
> TLS を使う場合は 2 と 3 を `https://` にします（§8）。

あわせて `deploy/k8s/base/config.yaml` の次の項目を自分の環境に合わせます。

- `network.internal_cidrs` … 内部ネットワークの CIDR（RFC1918 以外も指定できます）
- `network.origin` … Globe View の起点。おおまかな座標で十分です（正確な住所は不要）

---

## 2. コンテナイメージを用意する

イメージは 2 つです。

| イメージ | Dockerfile | 中身 |
|---|---|---|
| `ntv-go` | `Dockerfile.backend` | `ntv-api` / `ntv-collector` / `ntv-sflow-gen`（共通イメージ） |
| `ntv-web` | `Dockerfile` | Next.js の Web UI |

リポジトリのルートでビルドします。**Web イメージは必ず `NEXT_PUBLIC_DATA_MODE=api` でビルド**してください（`mock` のままだとブラウザ内でモックを生成し、API を使いません）。

```bash
docker build -f Dockerfile.backend -t ntv-go:dev .
```

```bash
docker build -t ntv-web:dev --build-arg NEXT_PUBLIC_DATA_MODE=api --build-arg NEXT_PUBLIC_API_BASE_URL=http://ntv.home.arpa .
```

> クラスタのノードが amd64 で、Apple Silicon の Mac でビルドする場合は `docker buildx build --platform linux/amd64 ...` を使ってください。

### 2-1. クラスタからイメージを使えるようにする

使っている環境に合わせて **いずれか 1 つ** を実行します。

**レジストリに push する（マネージド K8s・複数ノード）**

```bash
docker tag ntv-go:dev registry.example.com/ntv/ntv-go:0.1.0
```

```bash
docker tag ntv-web:dev registry.example.com/ntv/ntv-web:0.1.0
```

```bash
docker push registry.example.com/ntv/ntv-go:0.1.0
```

```bash
docker push registry.example.com/ntv/ntv-web:0.1.0
```

push したら `deploy/k8s/base/kustomization.yaml` の `images:` を書き換えます。

```yaml
images:
  - name: ntv-go
    newName: registry.example.com/ntv/ntv-go
    newTag: 0.1.0
  - name: ntv-web
    newName: registry.example.com/ntv/ntv-web
    newTag: 0.1.0
```

プライベートレジストリの場合は、`ntv` 名前空間に `imagePullSecrets` 用の Secret を作成し、各 Deployment に追加してください。

**kind**

```bash
kind load docker-image ntv-go:dev ntv-web:dev
```

**minikube**

```bash
minikube image load ntv-go:dev
```

```bash
minikube image load ntv-web:dev
```

**k3s（ノード上で実行）**

```bash
docker save ntv-go:dev ntv-web:dev | sudo k3s ctr images import -
```

**Docker Desktop の Kubernetes**：ローカルでビルドしたイメージがそのまま使えます。

> タグが `latest` 以外なので `imagePullPolicy` は `IfNotPresent` になり、ノードに読み込んだイメージがそのまま使われます。

---

## 3. モックモードでデプロイする（base）

sFlow 機器なしで、API 経由のモックデータを表示します。

```bash
kubectl apply -k deploy/k8s/base
```

```bash
kubectl -n ntv rollout status deploy/ntv-api deploy/ntv-web
```

```bash
kubectl -n ntv get pods,svc,ingress
```

ブラウザで `http://ntv.home.arpa/globe` を開きます。右上に `MOCK` と表示され、弧が動いていれば成功です。

モックのシナリオを変えるには `deploy/k8s/base/config.yaml` の `mock.scenario` を変更し、再度 `kubectl apply -k` します（ConfigMap 名のハッシュが変わるため、API の Pod は自動で作り直されます）。

### 3-1. Ingress を使わずに確認する場合

```bash
kubectl -n ntv port-forward svc/ntv-api 8080:8080
```

この場合は Web イメージを `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080` でビルドし、`config.yaml` の `cors_allowed_origins` に `http://localhost:3000` を追加したうえで、別のターミナルで次を実行します。

```bash
kubectl -n ntv port-forward svc/ntv-web 3000:3000
```

---

## 4. ライブモードでデプロイする（overlays/live）

実際のネットワーク機器から sFlow v5 を受信して表示します。

### 4-1. インベントリを編集する

`deploy/k8s/overlays/live/inventory.yaml` に、自分の環境の情報を書きます（形式は `configs/inventory.example.yaml` と同じです）。

- `exporters[].agent_address` … sFlow データグラム内の **agent address**（UDP の送信元 IP ではありません）
- `exporters[].role: boundary` と `boundary_if_index` … WAN インターフェース。Download / Upload の値に使われます
- `devices[].id` … 永続的な ID。IP アドレスを ID にすることはできません

すべて省略しても動きます（内部の端末は `Unresolved device` として表示されます）。

### 4-2. デプロイする

```bash
kubectl apply -k deploy/k8s/overlays/live
```

```bash
kubectl -n ntv get svc ntv-sflow
```

`EXTERNAL-IP` に割り当てられたアドレスが sFlow の送信先です。固定したい場合は `sflow-service.yaml` のアノテーション（MetalLB の例）を使ってください。

### 4-3. ネットワーク機器を設定する

各 sFlow エージェント（ルーター・スイッチ）の送信先（collector）を `<EXTERNAL-IP>:6343/udp` に設定します。設定方法は機器ごとに異なるため、各機器のマニュアルを参照してください。

### 4-4. 受信元を制限する（推奨）

UDP/6343 は認証も暗号化もありません。次のいずれか、または両方で送信元を絞ってください。

- `deploy/k8s/base/config.yaml` の `collector.allowed_sources` に機器の IP / CIDR を書く
- `sflow-service.yaml` の `loadBalancerSourceRanges` を使う（LB の実装が対応している場合）

> `externalTrafficPolicy: Local` は送信元 IP を保持するための設定です。これにより、**API の Pod が動いているノード**だけが LB からのトラフィックを受け付けます。`Cluster` に変更すると、どのノードでも受信できるようになりますが、送信元 IP が書き換わって `allowed_sources` が正しく判定できなくなります。

### 4-5. 確認する

```bash
kubectl -n ntv logs deploy/ntv-api -f
```

`"msg":"live mode"` の後に、コレクターのサマリーが 10 秒ごとに出力されれば受信しています。画面右上は `SFLOW` 表示になります。

---

## 5. デモ構成（overlays/demo）

実機を使わずにライブモードの経路（UDP → コレクター → 集計 → API → UI）を確認します。`sflow-gen` が、クラスタ内でモック由来の sFlow を `ntv-sflow:6343` に送信します。インベントリにはイメージに同梱されている `inventory.demo.yaml` を使います。

```bash
kubectl apply -k deploy/k8s/overlays/demo
```

この構成では `ntv-sflow` が ClusterIP になるため、LoadBalancer の実装は不要です。
デモを終えて live に切り替えるときは、先に `ntv-sflow-gen` を削除します。

```bash
kubectl -n ntv delete deploy/ntv-sflow-gen
```

```bash
kubectl apply -k deploy/k8s/overlays/live
```

---

## 6. 内部の CIDR と原点の変更

`deploy/k8s/base/config.yaml` を編集して再適用します。環境変数で上書きすることもできます（例：`NETWORK_INTERNAL_CIDRS`、`COLLECTOR_ALLOWED_SOURCES`。一覧は `.env.example` を参照）。

> Pod / Service の CIDR（例：`10.42.0.0/16`）は、「内部」として扱いたい場合だけ含めてください。ホームネットワークの CIDR と重なる場合は、より狭い範囲を指定します。

---

## 7. GeoIP（任意）

GeoIP がなくても動作します（宛先は「Unknown location」と表示されます）。国・都市・ASN を表示したい場合は、MaxMind の GeoLite2-City / GeoLite2-ASN（`.mmdb`、要アカウント）を用意します。ファイルが ConfigMap の上限（1 MiB）を超えるため、PVC に置きます。

1. `deploy/k8s/overlays/live/kustomization.yaml` の `components:` のコメントを外して適用します。

   ```bash
   kubectl apply -k deploy/k8s/overlays/live
   ```

2. 一時的な Pod で PVC にファイルを書き込みます（ストレージが RWO の場合は、API の Pod と同じノードに配置される必要があります。先に API を 0 台にしておくと確実です）。

   ```bash
   kubectl -n ntv scale deploy/ntv-api --replicas=0
   ```

   ```bash
   kubectl -n ntv apply -f - <<'EOF'
   apiVersion: v1
   kind: Pod
   metadata:
     name: geoip-loader
   spec:
     containers:
       - name: loader
         image: alpine:3.22
         command: ["sleep", "3600"]
         volumeMounts:
           - { name: geoip, mountPath: /data/geoip }
     volumes:
       - name: geoip
         persistentVolumeClaim: { claimName: ntv-geoip }
   EOF
   ```

   ```bash
   kubectl -n ntv wait --for=condition=Ready pod/geoip-loader
   ```

   ```bash
   kubectl -n ntv cp ./data/GeoLite2-City.mmdb geoip-loader:/data/geoip/GeoLite2-City.mmdb
   ```

   ```bash
   kubectl -n ntv cp ./data/GeoLite2-ASN.mmdb geoip-loader:/data/geoip/GeoLite2-ASN.mmdb
   ```

   ```bash
   kubectl -n ntv exec geoip-loader -- chmod 644 /data/geoip/GeoLite2-City.mmdb /data/geoip/GeoLite2-ASN.mmdb
   ```

   ```bash
   kubectl -n ntv delete pod geoip-loader
   ```

3. API を戻します。

   ```bash
   kubectl -n ntv scale deploy/ntv-api --replicas=1
   ```

ログに `GeoIP database not found` が出なくなれば読み込まれています。定期更新が必要な場合は、MaxMind の `geoipupdate` を CronJob で動かして同じ PVC を更新する方法があります（このリポジトリには含めていません）。データベースを更新した後は、API を再起動してください（`kubectl -n ntv rollout restart deploy/ntv-api`）。

---

## 8. TLS

1. 証明書を Secret として作成します（cert-manager を使っている場合は自動で作成できます）。

   ```bash
   kubectl -n ntv create secret tls ntv-tls --cert=tls.crt --key=tls.key
   ```

2. `deploy/k8s/base/ingress.yaml` の `tls:` のコメントを外します。
3. `config.yaml` の `cors_allowed_origins` を `https://ntv.home.arpa` に変更します。
4. Web イメージを `NEXT_PUBLIC_API_BASE_URL=https://ntv.home.arpa` で再ビルドします（WebSocket は自動的に `wss://` になります）。

---

## 9. 動作確認チェックリスト

| 確認内容 | コマンド / 操作 | 期待する結果 |
|---|---|---|
| Pod が起動している | `kubectl -n ntv get pods` | すべて `Running` / `READY 1/1` |
| API が応答する | `kubectl -n ntv exec deploy/ntv-api -- wget -qO- http://127.0.0.1:8080/api/v1/status` | `"mode":"mock"` または `"mode":"live"` |
| Ingress 経由で API に届く | `curl http://ntv.home.arpa/api/v1/status` | 上と同じ JSON |
| Globe View | ブラウザで `/globe` | 弧とマーカーが動き、右上が `MOCK` / `SFLOW` |
| 両ビューの連携 | 宛先を選び「Open in Home Network」 | Home View で送信元の端末がハイライトされる |
| sFlow の受信（live） | `kubectl -n ntv logs deploy/ntv-api` | コレクターのサマリーで受信数が増えていく |
| メトリクス（live） | `kubectl -n ntv port-forward svc/ntv-api 8080:8080` → `curl localhost:8080/metrics` | Prometheus 形式の値が返る |

`/healthz` と `/metrics` は Ingress に公開していません（クラスタ内からのみアクセスできます）。Prometheus でスクレイプする場合は、`ntv-api` Service の 8080 番ポート `/metrics` を対象にしてください。

---

## 10. 運用

**更新**：新しいタグでイメージを push し、`images:` を書き換えて `kubectl apply -k` します。API は `Recreate` で更新されるため、数秒間の中断が発生します。また、ライブ集計はメモリ上にあるため、再起動すると直近のウィンドウはリセットされます。

**設定変更**：`config.yaml` / `inventory.yaml` を編集して `kubectl apply -k` します（ConfigMap のハッシュが変わるため、Pod は自動で作り直されます）。

**削除**

```bash
kubectl delete -k deploy/k8s/overlays/live
```

（GeoIP の PVC も削除されます。PVC を残したい場合は、先に `components:` を外してから削除してください。）

---

## 11. トラブルシューティング

| 症状 | 原因と対処 |
|---|---|
| 画面右上が `MOCK` のまま（live のはず） | Web イメージが `NEXT_PUBLIC_DATA_MODE=mock` でビルドされています。`api` で再ビルドしてください |
| 画面は出るがデータが来ない / `Connecting` のまま | ブラウザの開発者ツールで `/api/v1/ws` を確認します。403 の場合は `cors_allowed_origins` と実際の URL（scheme・host・port）が一致していません。`localhost:8080` に接続している場合は、`NEXT_PUBLIC_API_BASE_URL` の指定を忘れてビルドしています |
| `ImagePullBackOff` / `ErrImageNeverPull` | §2-1 の手順でイメージがノードに読み込まれていないか、`images:` の名前・タグが一致していません |
| `CreateContainerConfigError: ... non-numeric user` | マニフェストの `runAsUser: 100` を削除していないか確認してください（イメージのユーザー `app` = UID 100） |
| API が `CrashLoopBackOff` | `kubectl -n ntv logs deploy/ntv-api --previous` を確認します。`invalid configuration` は設定キーの誤り（未知のキーはエラーになります）。`sFlow collector on ... did not start` は UDP ポートの競合です |
| `ntv-sflow` の `EXTERNAL-IP` が `<pending>` | LoadBalancer の実装がありません。MetalLB を導入するか、`type: NodePort` に変更し、機器の送信先を `<ノード IP>:<nodePort>` にしてください |
| live でステータスが `Waiting for sFlow` のまま | 機器の送信先 IP・ポート、ファイアウォール、`allowed_sources` を確認します。`externalTrafficPolicy: Local` の場合、LB が API の Pod のあるノードにだけ転送しているか確認してください |
| 端末がすべて `Unresolved device` と表示される | `inventory.yaml` の `devices[].addresses` が未設定です。`exporters` の `agent_address` も、データグラム内の値と一致しているか確認してください |
| IPv6 だけのクラスタで sFlow を受信できない | `collector.listen` を `"[::]:6343"` に変更してください（既定値は IPv4 の `0.0.0.0`） |

---

## 12. セキュリティ上の注意

- UI / API には認証がありません。Ingress は信頼できるネットワーク内だけに公開し、インターネットには公開しないでください。外部に公開する場合は、Ingress コントローラーの認証機能や OAuth2 Proxy などを前段に置いてください。
- 表示されるデータ（内部 IP、端末名、通信先）は機微な情報です。
- `collector.debug_flows_per_second` は 0 のままにしてください（ログにフロー情報が出力されます）。
- Pod は非 root（UID 100）、読み取り専用のルートファイルシステム、全 capability を drop した状態で動作します。
