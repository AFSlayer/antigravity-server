<div align="center">

# Antigravity Server

自分のAntigravityへの二つ目の玄関。  
モバイルWeb UIを直し、UIはGoogleの中継を通らず、安価なLinuxマシンで無人稼働します。

[![release](https://img.shields.io/github/v/release/AFSlayer/antigravity-server?style=flat-square&color=4f7cff)](https://github.com/AFSlayer/antigravity-server/releases/latest)
[![ci](https://img.shields.io/github/actions/workflow/status/AFSlayer/antigravity-server/ci.yml?branch=main&style=flat-square)](https://github.com/AFSlayer/antigravity-server/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

| 公式リモート | 同じサーバー、`agy-server`経由 |
| :---: | :---: |
| <img src="docs/assets/compare-official.png" width="380" alt="公式リモートブリッジ経由でスマートフォンに表示された会話一覧" /> | <img src="docs/assets/compare-agy.png" width="380" alt="同じ会話一覧をagy-server経由で表示。プロジェクトごとの新規会話ボタンと行ごとのケバブメニュー" /> |
| プロジェクトに`+`なし。会話に`⋮`なし。 | プロジェクトごとに新規会話、行ごとに削除・名前変更・ピン留め・アーカイブ。 |

<sub>ヘッドレスLinux 1台、玄関は2つ。数分違いで撮影。</sub>

[English](README.md) · [한국어](README.ko.md) · [中文](README.zh-CN.md) · [Português](README.pt-BR.md) · [Español](README.es.md)

</div>

---

## なぜAntigravity Serverなのか？（公式リモートブリッジとの比較）

Googleは`antigravity.google.com`で公式のリモートブリッジを提供開始しました。同じアカウントでログインすれば、Antigravityが起動していてリモートアクセスを許可した自分のマシンすべてに届きます。**「スマートフォンから自分のエージェントを使う」こと自体は、もはや本プロジェクトが担う話ではなく**、ヘッドレスLinuxサーバーもその一覧に現れます。

公式ブリッジがスマートフォンに届けるのは、デスクトップ用Webバンドルそのままです。`agy-server`の存在意義はここにあります — 同じAntigravityコアの前に**二つ目の直接玄関**として立ち、出ていくバンドルをタッチで使えるように書き換えます。

両者は排他的ではありません。`agy-server`が有効化するのも公式ブリッジが使う`remoteControlEnabled`設定ひとつなので、1台のマシンが両方を同時に提供できます — 都合のよいアドレスを使ってください。

| | 公式リモート（`antigravity.google.com`） | Antigravity Server (`agy-server`) |
| :--- | :--- | :--- |
| **モバイルWeb UI** | デスクトップバンドルそのまま | タッチ向け**ランタイムパッチ25件** |
| **会話管理** | モバイルで削除・ピン留め・アーカイブ不可 | ケバブメニューとタイトルバーから**削除、名前変更、ピン留め、アーカイブ** |
| **プロジェクト移動** | プロジェクト`(+)`ボタンなし；下部入力欄で切り替え | プロジェクト一覧ヘッダーに**`(+)`ボタンを復元** |
| **メッセージアクション** | Undo・Copyがホバーの裏に隠れる | タッチで**Undo（`↶`）・Copy（`📋`）を常時表示** |
| **iOSキーボード・入力** | 下部Safe Areaの余白が残り、フォーカス時に画面が揺れる；改行時のIME確定バグ | 上部バー固定、Safe Area縮小と会話高さ適応、質問モーダル操作の安定化、Enter時のCJK/日本語IME確定・改行の保護 |
| **ファイルアップロード** | 1MB RPCテキスト容量制限 | 大容量ログ・HAR・データセット向け**チャンクストリーミングアップローダー** |
| **接続経路** | Googleのサーバー経由で中継 | **直接接続** — 自分のドメイン、ローカルネットワーク、VPN |
| **サーバー再起動 / 切断** | 再起動時にCSRFトークンが失効し接続が途絶、手動リロードが必須 | **リロード不要の自動再接続** — CSRFトークンの永続化とgRPC status 14変換により再起動後も即座に自動復帰 |
| **メモリ保守** | 長時間稼働時にメモリが無制限に累積（~4GB以上） | **日次アイドル再起動** — 作業を中断せず、アイドル時に言語サーバーを自動再起動してメモリを約450MBにリセット |
| **Googleアカウントなしのアクセス** | 不可 — アカウントが関門 | 自分のパスワード（PBKDF2）、セッション、レート制限 |

---

## クイックスタート

### オプション1: Linuxサーバー / クラウドVPS（推奨）

ヘッドレスLinuxインスタンス（Oracle Cloud Free Tier、AWS、DigitalOcean、自宅サーバーなど）で実行：

```bash
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash
```

インストーラーの動作：
1. ドメイン（例: `agy.example.com`）とワークスペースのパスを入力します。
2. Google公式ビルドバケット（`storage.googleapis.com`）から`language_server`バイナリを直接ダウンロードします。（Googleバイナリの再配布は行いません。）
3. Caddyによる自動HTTPS設定、systemdサービス登録、アクセスパスワード設定を完了します。

#### Googleアカウント認証
サーバー初回アクセス時：
- **Web UIから直接ログイン**: ブラウザでアクセス後、**設定（Settings）**メニューからGoogleログインを完了します。
- **既存トークンのコピー（任意）**: すでにデスクトップ環境でログイン済みの場合は、トークンをコピーして認証をスキップすることも可能です：
  ```bash
  scp ~/.gemini/jetski-standalone-oauth-token user@your-server:~/.gemini/
  ```

---

### オプション2: デスクトップコンパニオン（macOS、Windows、Linuxデスクトップ）

ローカルPCで実行中のAntigravityを同一ネットワーク上のスマートフォンに共有：

```bash
# macOS & Linux
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.sh | bash
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.ps1 | iex
```

`agy-server`がQRコード付きのローカルコントロールパネルを開きます。同一Wi-Fi上のスマートフォンでQRコードをスキャンすると、パスワード入力なしで接続できます。

<div align="center">
<img src="docs/assets/control-panel.png" width="320" alt="Control Panel" />
</div>

---

## モバイルPWA設定（ホーム画面に追加）

Antigravity ServerはPWA（Progressive Web App）規格をサポートしています。モバイルブラウザで「ホーム画面に追加」すると、**アドレスバーやツールバーのない全画面スタンドアロンアプリ**として起動します：

- **iOS (Safari)**: 下部の**共有ボタン（`⎋`）**をタップ → **「ホーム画面に追加」**を選択
- **Android (Chrome)**: 右上の**メニュー（`⋮`）**をタップ → **「アプリをインストール」**または**「ホーム画面に追加」**を選択

> [!TIP]
> ホーム画面アイコンから起動すると、仮想キーボード起動時の画面揺れを防ぎ、**0pxキーボード密着パッチ**が完璧に動作します。

---

## 主な機能

### ⚡ モバイル特化UXパッチ
- **タッチ操作アクション**: メッセージ吹き出しにUndo（`↶`）およびCopy（`📋`）ボタンを常時表示。
- **完全な会話管理**: タイトルバーメニューからの会話削除、リストメニューからのピン留め・アーカイブに対応。
- **正確な仮想キーボード追従**: キーボード表示時に上部バーを固定し、Safe Area余白を0pxに縮小して会話高さを最適化。
- **スクロールアンカリング＆トップガード**: 長い会話で過去ログ取得時に発生する連続無限フェッチを防ぎ、上部スクロール位置を安定して保持。

<div align="center">
<img src="docs/assets/demo.gif" width="320" alt="スマートフォンのブラウザで動作するパッチ適用済みモバイルWeb UI" />
</div>

---

### 📁 大容量ファイルチャンクストリーミングアップロード
公式Antigravityの1MB RPC制限を解除し、大容量ログやデータセットをワークスペースへ直接ストリーミング転送します：

<div align="center">
<img src="docs/assets/upload.gif" width="560" alt="大容量ファイルストリーミングアップローダーデモ" />
</div>

---

### 🖥️ デスクトップ＆タブレットWebインターフェース
スマートフォンだけでなく、ノートPCやデスクトップブラウザからも快適に利用できます：

<div align="center">
<img src="docs/assets/desktop.png" width="700" alt="デスクトップブラウザで動作するAntigravity Web UI" />
</div>

---

### 🔄 無停止自動更新と日次メモリ保守
ヘッドレスLinuxサーバーにおいて、`agy-server`はバックグラウンド自動更新サービスを提供します：
- Google公式リリースバケットを毎日確認し、新しい`language_server`バージョンを検出。
- コアバイナリを無停止のアトミック方式で安全に置換。
- **日次アイドル再起動（Daily Idle Restart）**: 最新バージョンの場合、アクティブなストリームが0件かつ15分以上リクエストがないアイドル時間帯に言語サーバーを安全に再起動し、蓄積された会話履歴とメモリをリセットします。作業中やトラフィックが検出された場合は再起動を10分間安全に遅延（defer）させます。
- 手動更新チェック＆実行: `agy-server update`

---

### 🔁 リロード不要の自動再接続とセッション維持
言語サーバーが再起動した際（自動更新やサービス再起動）や一時的なネットワーク切断時：
- **CSRFトークンの永続化**: 再起動後も同一の認証トークンを保持し、セッション不一致による切断を防止。
- **gRPC-Webプロトコル変換**: アップストリーム一時切断時に502 HTMLではなく標準`grpc-status: 14` (Unavailable)を返却し、フロントエンドの状態購読ストリームがブラウザのリロードなしで即座に自動復帰。
- **再接続後の切断警告バナー自動解除**: サーバー再接続完了後、コンポーザー下部に残存する「Lost connection」警告バナーを即座に検知して自動消去。
- **読み込みスピナー停止自動復帰**: モバイルWebKitのHTTP/2ストリーム遅延によりスピナーが8秒以上停止した場合、クライアントウォッチドッグが自動的に接続を再読み込みして復帰。

---

### 📝 Web UI でのルール＆スキルエディタ
サーバーのターミナルに直接接続しなくても、Webブラウザからエージェント指示（`~/.gemini/GEMINI.md`、`~/.gemini/config/skills/`）やプロジェクトルールを編集できます：
- **Settings → Customizations** メニューに移動します。
- ルールまたはスキルの横にある **Edit** ボタンをクリックしてインラインエディタを開きます。
- 編集後 **Save** をクリックすると、ホストのファイルシステムにアトミックに保存され即時反映されます。

---

## 本番リバースプロキシ設定（Caddy / Nginx）

エージェントのリアルタイムストリーミング応答（SSE）およびWebSocket通信、大容量アップロードのため、プロキシの**バッファリング無効化**と**WebSocketアップグレード**設定が必要です：

### Caddy
```caddyfile
agy.example.com {
    encode zstd gzip

    reverse_proxy 127.0.0.1:8765 {
        flush_interval -1
    }
}
```

### Nginx
```nginx
server {
    listen 443 ssl http2;
    server_name agy.example.com;

    # 大容量チャンクアップロードを許可
    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8765;
        proxy_http_version 1.1;

        # WebSocketサポート
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        # リアルタイムトークンストリーミング用のバッファリング無効化（必須）
        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;

        # クライアント実IPの転送
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

> [!IMPORTANT]
> リバースプロキシの背後で実行する場合、総当たり攻撃防御（IPロックアウト）がクライアントの実IPを正しく識別できるように `--trusted-proxies 127.0.0.1/32`（または環境変数 `AGY_TRUSTED_PROXIES=127.0.0.1/32`）を設定してください。

---

## 動作原理

Antigravity内部には`language_server`という独立バイナリが含まれています。`--standalone`フラグで起動すると、ローカル`127.0.0.1`にWebインターフェースを提供します。

`agy-server`はこのバイナリの前段でリバースプロキシとして動作します：

```
  スマートフォン / タブレット / PC ブラウザ
                │
                ▼ HTTPS (Port 443 / 8765)
  ┌──────────────────────────────────────────────┐
  │ agy-server (リバースプロキシ & 認証)         │
  │  - PBKDF2セッション管理 & レートリミット     │
  │  - チャンクストリーミングアップローダー      │
  │  - リアルタイムWebバンドルパッチ適用         │
  └──────────────────────┬───────────────────────┘
                         │ localhost
                         ▼
  ┌──────────────────────────────────────────────┐
  │ language_server --standalone                 │
  │  - 公式Antigravityコア & エージェントエンジン│
  │  - ターミナル、ファイルツリー、Composer      │
  └──────────────────────┬───────────────────────┘
                         │ gRPC
                         ▼
                Google CloudCode API
```

---

## モバイルUXパッチ詳細

公式リモートブリッジや`agy-server`経由で提供されるWebバンドルは本来デスクトップ向けです。`agy-server`は[`internal/patches/registry.go`](internal/patches/registry.go)に定義されたパッチにより、通信経路上で動的にバンドルを書き換えて最適化します。レジストリには45件のパッチが登録されており、そのうち25件がタッチ・モバイル専用、残りはアップロード、ナビゲーション、サインイン、キャッシュ無効化などを担当しています。代表的なパッチ一覧：

| 分類 | デスクトップバンドルのデフォルト動作 | agy-server パッチ適用後の動作 |
| :--- | :--- | :--- |
| **ナビゲーション** | モバイル画面でプロジェクトの`(+)`ボタンが非表示 | 各プロジェクト行の横に`(+)`新規会話ボタンを復元 |
| **会話管理** | タッチ端末で削除・ピン留め・アーカイブが利用不可 | `⋮`ケバブメニューおよびタイトルバーに削除・ピン留め・アーカイブを追加 |
| **メッセージ操作** | ホバー時のみUndo・Copyボタンが表示 | タッチ端末でUndo（`↶`）およびCopy（`📋`）ボタンを常時表示 |
| **仮想キーボード＆スクロール** | iOS Safariでビューポートの揺れ・下部余白が発生、長い会話の上部スクロールで連続フェッチ嵐が発生 | visualViewportオフセット追従、Safe Area 0px縮小、会話高レイアウト固定、CSSスクロールアンカリング、上部スクロールガードによる連続フェッチ遮断 |
| **ファイルアップロード** | 1MB RPC制限により大容量ログやデータセットの送信に失敗 | チャンクストリーミングエンドポイントによりディスクへ直接非同期書き込み |
| **タッチ操作感** | 300msタップ遅延およびダブルタップズームが発生 | `touch-action: manipulation`により即座のタップ反応を保証 |
| **接続安定性とバナー** | 再接続成功後も「Lost connection」バナーが残り続ける、またはモバイルWebKitでHTTP/2ストリームが停止する | サーバー疎通確認時に警告バナーを自動消去、8秒以上スピナー停止時にクライアントウォッチドッグで自動復帰 |
| **入力制御** | モバイルEnterで即送信またはIME組み合わせ破損、スラッシュコマンド存在時に行頭移動が崩れる | ネイティブ改行維持、IME未確定文字の保護、スラッシュコマンド共存時のCmd+Left(macOS)/Home(全OS)行頭移動およびCtrl+Left単語移動の保護、Cmd/Ctrl+Enterで送信 |
| **モデル選択** | モデルタップ時にメニューが即時閉じる | タップ時にreasoning effortサブメニューを正常に展開 |

`agy-server doctor`コマンドで、インストール済みバンドルに対するパッチ整合性を確認できます。

---

## CLIコマンド

```
agy-server                      デスクトップコンパニオンモードで起動（ローカルNW）
agy-server serve                ヘッドレスサーバーデーモンとして実行
agy-server update               Google公式最新language_serverの確認と更新
agy-server doctor               パッチ整合性およびシステム状態の診断
agy-server passwd [password]    Webアクセスパスワードの設定・変更
agy-server sessions [revoke]    アクティブセッションの確認・全ログアウト
agy-server config [flags]       config.json設定の管理
```

---

## セキュリティ

- **パスワード保護**: PBKDF2-SHA256（200,000回繰り返し計算）による一方向ハッシュ化。
- **セッショントークン**: 256ビット暗号学的乱数トークンを使用、ディスクにはSHA-256ハッシュのみ保存。
- **CSRFトークンの永続化と正規化**: 所有者専用権限（`0600`）で安全に保存され、プロキシ経由で正規化注入されることで、サーバー再起動時もセッション拒否なく安全に接続を維持します。
- **ブルートフォース保護**: 5回以上ログイン失敗時にIPを一時ロック（5分〜30分）。
- **アップロードの隔離**: ファイルアップロードは指定されたプロジェクトディレクトリ配下に厳格に制限され、パストラバーサル（`../`）は拒否されます。
- **信頼できるプロキシ**: Nginx、Caddy、Cloudflare等の背後で実行する場合は`--trusted-proxies`を指定してヘッダー偽装を防止します。

---

## ライセンス

[Apache-2.0](LICENSE). Not affiliated with or endorsed by Google. See [DISCLAIMER.md](DISCLAIMER.md).
