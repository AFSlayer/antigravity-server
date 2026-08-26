<div align="center">

# Antigravity Server

Uma segunda porta de entrada para o seu próprio Antigravity.  
Corrige a interface web mobile, dispensa o relay do Google e roda sozinho num Linux barato.

[![release](https://img.shields.io/github/v/release/AFSlayer/antigravity-server?style=flat-square&color=4f7cff)](https://github.com/AFSlayer/antigravity-server/releases/latest)
[![ci](https://img.shields.io/github/actions/workflow/status/AFSlayer/antigravity-server/ci.yml?branch=main&style=flat-square)](https://github.com/AFSlayer/antigravity-server/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

| Remoto oficial | O mesmo servidor, via `agy-server` |
| :---: | :---: |
| <img src="docs/assets/compare-official.png" width="380" alt="A lista de conversas num celular pela ponte remota oficial" /> | <img src="docs/assets/compare-agy.png" width="380" alt="A mesma lista via agy-server, com botão de nova conversa em cada projeto e menu kebab em cada linha" /> |
| Sem `+` nos projetos. Sem `⋮` nas conversas. | Nova conversa por projeto e excluir / renomear / fixar / arquivar por linha. |

<sub>Uma máquina Linux headless, duas portas de entrada, gravadas com minutos de diferença.</sub>

[English](README.md) · [한국어](README.ko.md) · [中文](README.zh-CN.md) · [日本語](README.ja.md) · [Español](README.es.md)

</div>

---

## Por que Antigravity Server? (vs Ponte Remota Oficial)

O Google agora oferece uma ponte remota oficial em `antigravity.google.com`: entre com a mesma conta e você alcança todas as suas máquinas que estejam rodando o Antigravity com acesso remoto habilitado. **Chegar ao seu próprio agente pelo celular já não é algo que este projeto precise fornecer** — e um servidor Linux headless também aparece nessa lista.

O que a ponte oficial entrega ao seu celular é o bundle web de desktop, sem alterações. É aí que o `agy-server` se justifica: ele fica na frente do mesmo núcleo do Antigravity como uma **segunda porta direta** e reescreve esse bundle na saída, para que uma tela de toque consiga realmente usá-lo.

Os dois não são exclusivos. O `agy-server` apenas habilita a mesma configuração `remoteControlEnabled` que a ponte oficial usa, então uma máquina serve os dois ao mesmo tempo — use o endereço que preferir.

| | Remoto oficial (`antigravity.google.com`) | Antigravity Server (`agy-server`) |
| :--- | :--- | :--- |
| **Interface web mobile** | O bundle de desktop como está | **42 patches de runtime** para toque |
| **Controle de conversas** | Sem excluir, fixar ou arquivar no celular | **Excluir, Renomear, Fixar e Arquivar** pelo menu kebab e pela barra de título |
| **Navegação de projetos** | Sem botão `(+)`; troca pelo input inferior | **Botão `(+)` restaurado** no cabeçalho dos projetos |
| **Ações de mensagem** | Desfazer e Copiar escondidos sob hover | **Desfazer (`↶`) e Copiar (`📋`)** sempre visíveis no toque |
| **Teclado no iOS** | Espaço vazio no Safe Area; tela pula ao focar | Colapso do Safe Area e rastreamento de viewport com o teclado aberto |
| **Upload de arquivos** | Limite de 1MB por RPC | **Upload por streaming fragmentado** para logs, HARs e datasets grandes |
| **Caminho da conexão** | Retransmitido pelos servidores do Google | **Direto** — seu próprio domínio, LAN ou VPN |
| **Quem consegue entrar** | Quem tiver aquela conta Google | Sua própria senha (PBKDF2), sessões e rate-limiting |
| **Rodar headless** | Você mesmo configura | Um instalador: unidade systemd, HTTPS via Caddy, auto-atualizador do `language_server` |

---

## Início Rápido

### Opção 1: Servidor Linux / VPS na Nuvem (Recomendado)

Execute o Antigravity em uma instância Linux headless (Oracle Cloud Free Tier, AWS, DigitalOcean ou servidor caseiro):

```bash
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash
```

O instalador:
1. Solicita seu domínio (ex: `agy.example.com`) e o diretório do workspace.
2. Baixa o `language_server` diretamente do bucket oficial do Google (`storage.googleapis.com`).
3. Configura o Caddy para HTTPS automático, cria o serviço systemd e define a senha de acesso.

#### Autenticação com o Google
Ao acessar o servidor pela primeira vez:
- **Login Direto pela Web**: Acesse a interface no navegador, vá em **Settings** e faça login com sua conta Google.
- **Ou Copiar Token Existente (Opcional)**: Caso já tenha feito login no desktop local:
  ```bash
  scp ~/.gemini/jetski-standalone-oauth-token user@seu-servidor:~/.gemini/
  ```

---

### Opção 2: Modo Desktop Companion (macOS, Windows, Linux Desktop)

Para compartilhar o Antigravity local na mesma rede Wi-Fi:

```bash
# macOS & Linux
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.sh | bash
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.ps1 | iex
```

O `agy-server` abre um painel de controle com QR code para conexão rápida pelo celular sem senha.

<div align="center">
<img src="docs/assets/control-panel.png" width="320" alt="Control Panel" />
</div>

---

## Configuração PWA Mobile (Adicionar à Tela de Início)

O Antigravity Server suporta o padrão Progressive Web App (PWA). Ao adicioná-lo à tela de início do smartphone, ele roda em **tela cheia sem barra de endereços**:

- **iOS (Safari)**: Toque em **Compartilhar (`⎋`)** → Selecione **Adicionar à Tela de Início**.
- **Android (Chrome)**: Toque no **Menu (`⋮`)** → Selecione **Instalar aplicativo** ou **Adicionar à tela inicial**.

> [!TIP]
> Executar como PWA garante que o patch de **ajuste 0px do teclado virtual** funcione com máxima suavidade.

---

## Principais Recursos

### ⚡ Patches de Interface para Mobile
- **Controles por Toque**: Botões Desfazer (`↶`) e Copiar (`📋`) permanentemente visíveis nos balões de mensagem.
- **Gerenciamento de Conversas**: Exclua conversas pela barra superior e fixe ou arquive pelo menu lateral.
- **Ajuste Preciso do Teclado**: Reduz o Safe Area para 0px assim que o teclado virtual aparece.

<div align="center">
<img src="docs/assets/demo.gif" width="320" alt="A interface web mobile com patches, num navegador de celular" />
</div>

---

### 📁 Upload de Arquivos Grandes por Streaming
Transfira arquivos pesados, logs e datasets diretamente para o workspace sem o limite de 1MB:

<div align="center">
<img src="docs/assets/upload.gif" width="560" alt="Demonstração do uploader por streaming" />
</div>

---

### 🖥️ Interface Web para Desktop e Tablet
Interface perfeitamente responsiva para navegadores de computadores e tablets:

<div align="center">
<img src="docs/assets/desktop.png" width="700" alt="Antigravity Web UI no navegador desktop" />
</div>

---

### 🔄 Atualizações Automáticas sem Quedas
Em servidores Linux headless, o `agy-server` inclui serviço de atualização automática:
- Verifica diariamente novas versões oficiais do `language_server`.
- Substitui o binário de forma atômica e segura.
- Verificação manual: execute `agy-server update`.

---

## Configuração de Proxy Reverso (Caddy / Nginx)

Para habilitar streaming em tempo real (SSE), WebSockets e uploads grandes, desative o buffer do proxy:

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

    client_max_body_size 0;

    location / {
        proxy_pass http://127.0.0.1:8765;
        proxy_http_version 1.1;

        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";

        proxy_buffering off;
        proxy_cache off;
        proxy_read_timeout 86400s;

        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

> [!IMPORTANT]
> Configure `--trusted-proxies 127.0.0.1/32` (ou a variável `AGY_TRUSTED_PROXIES=127.0.0.1/32`) para que a proteção contra força bruta identifique o IP real do usuário.

---

## Comandos CLI

```
agy-server                      Inicia no modo desktop companion (rede local)
agy-server serve                Executa como daemon em servidor headless
agy-server update               Verifica e atualiza o language_server oficial
agy-server doctor               Diagnostica o estado do sistema e patches
agy-server passwd [password]    Define ou altera a senha de acesso web
agy-server sessions [revoke]    Lista sessões ativas ou desconecta todos os aparelhos
agy-server config [flags]       Gerencia configurações em config.json
```

---

## Licença

[Apache-2.0](LICENSE). Não afiliado nem endossado pelo Google. Consulte [DISCLAIMER.md](DISCLAIMER.md).
