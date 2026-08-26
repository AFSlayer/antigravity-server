<div align="center">

# Antigravity Server

Una segunda puerta de entrada a tu propio Antigravity.  
Arregla la interfaz web móvil, se salta el relay de Google y funciona solo en un Linux barato.

[![release](https://img.shields.io/github/v/release/AFSlayer/antigravity-server?style=flat-square&color=4f7cff)](https://github.com/AFSlayer/antigravity-server/releases/latest)
[![ci](https://img.shields.io/github/actions/workflow/status/AFSlayer/antigravity-server/ci.yml?branch=main&style=flat-square)](https://github.com/AFSlayer/antigravity-server/actions/workflows/ci.yml)
[![license](https://img.shields.io/badge/license-Apache--2.0-blue?style=flat-square)](LICENSE)

| Remoto oficial | El mismo servidor, vía `agy-server` |
| :---: | :---: |
| <img src="docs/assets/compare-official.png" width="380" alt="La lista de conversaciones en un móvil a través del puente remoto oficial" /> | <img src="docs/assets/compare-agy.png" width="380" alt="La misma lista vía agy-server, con botón de nueva conversación en cada proyecto y menú kebab en cada fila" /> |
| Sin `+` en los proyectos. Sin `⋮` en las conversaciones. | Nueva conversación por proyecto y eliminar / renombrar / fijar / archivar por fila. |

<sub>Una máquina Linux headless, dos puertas de entrada, grabadas con minutos de diferencia.</sub>

[English](README.md) · [한국어](README.ko.md) · [中文](README.zh-CN.md) · [日本語](README.ja.md) · [Português](README.pt-BR.md)

</div>

---

## ¿Por qué Antigravity Server? (vs Puente Remoto Oficial)

Google ya ofrece un puente remoto oficial en `antigravity.google.com`: inicia sesión con la misma cuenta y llegas a todas tus máquinas que estén ejecutando Antigravity con el acceso remoto habilitado. **Llegar a tu propio agente desde el móvil ya no es algo que este proyecto tenga que aportar** — y un servidor Linux headless también aparece en esa lista.

Lo que el puente oficial entrega a tu móvil es el bundle web de escritorio, sin cambios. Ahí está el valor de `agy-server`: se coloca delante del mismo núcleo de Antigravity como una **segunda puerta directa** y reescribe ese bundle de salida para que una pantalla táctil pueda usarlo de verdad.

Ambos no son excluyentes. `agy-server` solo activa el mismo ajuste `remoteControlEnabled` que usa el puente oficial, así que una misma máquina sirve las dos vías — usa la dirección que te convenga.

| | Remoto oficial (`antigravity.google.com`) | Antigravity Server (`agy-server`) |
| :--- | :--- | :--- |
| **Interfaz web móvil** | El bundle de escritorio tal cual | **42 parches en tiempo de ejecución** para táctil |
| **Control de conversaciones** | Sin eliminar, fijar ni archivar en móvil | **Eliminar, Renombrar, Fijar y Archivar** desde el menú kebab y la barra de título |
| **Navegación de proyectos** | Sin botón `(+)`; se cambia desde el input inferior | **Botón `(+)` restaurado** en la cabecera de proyectos |
| **Acciones de mensaje** | Deshacer y Copiar ocultos tras el hover | **Deshacer (`↶`) y Copiar (`📋`)** siempre visibles al tacto |
| **Teclado en iOS** | Queda hueco en el Safe Area; saltos de viewport al enfocar | Colapso del Safe Area y seguimiento del viewport con el teclado abierto |
| **Subida de archivos** | Límite de 1MB por RPC | **Subida por fragmentos** para logs, HARs y datasets grandes |
| **Ruta de conexión** | Retransmitida por los servidores de Google | **Directa** — tu propio dominio, LAN o VPN |
| **Quién puede entrar** | Quien tenga esa cuenta de Google | Tu propia contraseña (PBKDF2), sesiones y límite de intentos |
| **Ejecutar headless** | Lo montas tú | Un instalador: unidad systemd, HTTPS con Caddy, auto-actualizador de `language_server` |

---

## Inicio Rápido

### Opción 1: Servidor Linux / VPS en la Nube (Recomendado)

Ejecuta Antigravity en una instancia Linux headless (Oracle Cloud Free Tier, AWS, DigitalOcean o servidor local):

```bash
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install.sh | bash
```

El instalador:
1. Solicita tu dominio (ej. `agy.example.com`) y directorio de trabajo.
2. Descarga `language_server` directamente desde el bucket oficial de Google (`storage.googleapis.com`).
3. Configura Caddy para HTTPS automático, crea un servicio systemd y define la contraseña de acceso.

#### Autenticación con Google
Al acceder al servidor por primera vez:
- **Inicio de sesión directo en la Web**: Abre la interfaz, ve a **Settings** y completa el inicio de sesión de Google directamente en tu navegador.
- **O copiar token existente (Opcional)**: Si ya iniciaste sesión en tu escritorio:
  ```bash
  scp ~/.gemini/jetski-standalone-oauth-token user@tu-servidor:~/.gemini/
  ```

---

### Opción 2: Compañero de Escritorio (macOS, Windows, Linux Desktop)

Para compartir tu instancia local en la misma red Wi-Fi:

```bash
# macOS & Linux
curl -fsSL https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.sh | bash
```

```powershell
# Windows (PowerShell)
irm https://raw.githubusercontent.com/AFSlayer/antigravity-server/main/scripts/install-desktop.ps1 | iex
```

`agy-server` abre un panel de control con un código QR para conectar tu móvil sin escribir contraseña.

<div align="center">
<img src="docs/assets/control-panel.png" width="320" alt="Control Panel" />
</div>

---

## Configuración PWA Móvil (Añadir a Pantalla de Inicio)

Antigravity Server soporta el estándar Progressive Web App (PWA). Al añadirlo a la pantalla de inicio, se abre en **pantalla completa sin barra de direcciones**:

- **iOS (Safari)**: Pulsa **Compartir (`⎋`)** → Selecciona **Añadir a pantalla de inicio**.
- **Android (Chrome)**: Pulsa **Menú (`⋮`)** → Selecciona **Instalar aplicación** o **Añadir a pantalla principal**.

> [!TIP]
> Ejecutarlo como PWA asegura que el parche de **ajuste a 0px del teclado virtual** funcione con total fluidez.

---

## Características Principales

### ⚡ Parches de Experiencia Móvil
- **Controles Táctiles**: Botones Deshacer (`↶`) y Copiar (`📋`) permanentemente visibles.
- **Gestión Completa de Chats**: Elimina conversaciones desde la barra superior y fija o archiva desde el menú desplegable.
- **Seguimiento Preciso de Teclado**: Colapsa el Safe Area a 0px al abrir el teclado en pantalla.

<div align="center">
<img src="docs/assets/demo.gif" width="320" alt="La interfaz web móvil con parches, en un navegador de móvil" />
</div>

---

### 📁 Subida de Archivos Grandes por Streaming
Supera el límite de 1MB de Antigravity transmitiendo archivos pesados directamente al espacio de trabajo:

<div align="center">
<img src="docs/assets/upload.gif" width="560" alt="Demostración de subida por streaming" />
</div>

---

### 🖥️ Interfaz Web para Escritorio y Tablet
Disfruta de una experiencia fluida tanto en móviles como en navegadores de sobremesa:

<div align="center">
<img src="docs/assets/desktop.png" width="700" alt="Antigravity Web UI en navegador de escritorio" />
</div>

---

### 🔄 Actualizaciones Automáticas sin Caídas
En servidores Linux headless, `agy-server` incluye un servicio de actualización automática:
- Comprueba diariamente las nuevas versiones oficiales de `language_server`.
- Reemplaza el binario de forma atómica sin interrumpir el servicio.
- Comprobación manual: ejecuta `agy-server update`.

---

## Configuración de Proxy Inverso (Caddy / Nginx)

Para permitir streaming en tiempo real (SSE), WebSockets y subidas pesadas, desactiva el almacenamiento en búfer:

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
> Configura `--trusted-proxies 127.0.0.1/32` (o la variable `AGY_TRUSTED_PROXIES=127.0.0.1/32`) para que la protección contra fuerza bruta identifique la IP real del cliente.

---

## Comandos CLI

```
agy-server                      Inicia en modo compañero de escritorio (red local)
agy-server serve                Ejecuta como demonio en servidor headless
agy-server update               Comprueba y actualiza language_server a la última versión
agy-server doctor               Diagnostica el estado del sistema y parches
agy-server passwd [password]    Establece o cambia la contraseña web
agy-server sessions [revoke]    Lista sesiones activas o cierra sesión en todos los dispositivos
agy-server config [flags]       Gestiona la configuración en config.json
```

---

## Licencia

[Apache-2.0](LICENSE). No afiliado ni respaldado por Google. Consulta [DISCLAIMER.md](DISCLAIMER.md).
