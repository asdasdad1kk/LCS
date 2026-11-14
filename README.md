# LCS — Layout Control System

Gestor de layouts DWG para estudio. Un solo `.exe`: servidor local y ventana
nativa frameless con controles propios. El trabajo real ocurre en **ramas**: se descarga
el plano, se edita en AutoCAD, se vuelve a subir y recién ahí se integra a `main`.

```
 rama work/*  ──descargar ZIP──▶  AutoCAD  ──subir DWG──▶  rama  ──integrar──▶  main
```

`main` es la verdad publicada. Nadie escribe sobre `main` desde su máquina: todo
pasa por una rama revisable.

---

## Qué resuelve

- **Trazabilidad**: cada plano tiene historial de revisiones con autor, fecha y
  mensaje. Se puede ver cualquier revisión en el visor y restaurarla en `main`
  sin reescribir historia.
- **Trabajo en paralelo**: dos personas pueden tomar el mismo plano en ramas
  distintas. Al integrar, LCS detecta si `main` cambió el mismo archivo desde que
  se abrió la rama y frena el merge (409) en vez de pisar cambios.
- **Cero infraestructura**: no hace falta servidor web ni base de datos. El
  repositorio vive en una carpeta compartida (NAS) y cada máquina tiene su clon
  local. Solo el `push` toca la red.
- **Visor**: los DWG se abren en el navegador (LibreDWG WASM) con sus layouts,
  sin AutoCAD instalado.

## Instalación

1. Copiar `LCS.exe` y `.env` a la máquina.
2. Editar `LCS_WORKSPACE` en `.env` con la carpeta de proyectos. Si las herramientas no están en `PATH`, también definir `GO_BIN`, `NODE_BIN` y `NPM_BIN` antes de compilar.
3. Doble clic. Se abre la ventana de LCS; la interfaz funciona dentro de una
   WebView de Wails y la API permanece disponible en `127.0.0.1`.

### Dónde vive el workspace

Hay dos formas, en este orden de prioridad:

1. **Variable de entorno** `LCS_WORKSPACE` (útil para servidores o para probar).
2. **Archivo de configuración** `%LOCALAPPDATA%\LCS\config.json`, que es lo que
   usa el `.exe` al abrirse con doble clic (Explorer no siempre hereda variables
   nuevas hasta reiniciar sesión):

```json
{
  "workspace": "D:\\LCS",
  "port": 7809,
  "actor": "danie"
}
```

Hoy está apuntando a **`D:\LCS`**. Cuando el NAS esté disponible, se cambia esa
línea por la ruta del share (`"\\\\NAS01\\proyectos\\LCS"`) y el resto sigue
igual: los repositorios son portables, solo hay que copiar la carpeta
`projects/` y listo. La base local y el clon se reconstruyen solos.

### Variables de entorno

| Variable        | Obligatoria | Por defecto                    | Para qué |
|-----------------|-------------|--------------------------------|----------|
| `LCS_WORKSPACE` | sí*         | `config.json`                  | Carpeta compartida del NAS. Contiene `projects/<id>/repo.git`. |
| `LCS_DATA_DIR`  | no          | `%LOCALAPPDATA%\LCS`           | Estado local: base SQLite, clones, `config.json` y logs. **Nunca** debe estar en OneDrive ni en el share. |
| `LCS_PORT`      | no          | `7809`                         | Puerto en `127.0.0.1`. Si está ocupado, LCS asume que ya corre y solo abre el navegador. |
| `LCS_ACTOR`     | no          | usuario de Windows             | Nombre que firma los commits. |
| `LCS_SEED_DIR`  | no          | —                              | Carpeta que se importa al crear un proyecto (útil para arrancar con un set existente). |

\* Obligatoria salvo que `config.json` defina `workspace`.

Ejemplo (PowerShell, para dejar fijo el workspace):

```powershell
[Environment]::SetEnvironmentVariable('LCS_WORKSPACE', '\\NAS01\proyectos\LCS', 'User')
[Environment]::SetEnvironmentVariable('LCS_ACTOR', 'danie', 'User')
```

### Estructura en el NAS

```
<LCS_WORKSPACE>/
  projects/
    torre-delta/
      repo.git/          <- repositorio bare (la única copia compartida)
      .lcs/project.json
  .lcs/locks/            <- locks entre máquinas
```

Estructura local (`%LOCALAPPDATA%\LCS`):

```
lcs.db                   <- índice SQLite (reconstruible desde git)
repos/<project>/         <- clon de trabajo de esta máquina
logs/lcs.log
```

Si se borra `lcs.db`, LCS lo reconstruye la próxima vez leyendo el repositorio.
Git es la fuente de verdad, no la base.

## Flujo de trabajo

1. **Abrir tarea** — desde el panel o desde un plano. Elegís qué DWGs entran en
   la rama y un título ("Actualizar rótulo planta baja"). Se crea
   `work/<título>-<hash>` a partir de `main`.
2. **Descargar ZIP** — trae los DWG en la versión base más `LEEME.txt` y
   `TAREA-LCS.json`. Todo lo que esté en `drawings/` del ZIP va a parar a
   `drawings/` del repositorio al subir.
3. **Editar** — en AutoCAD, con el mismo nombre de archivo. LCS no modifica
   geometría: versiona, indexa, compara y distribuye.
4. **Subir** — se valida la cabecera DWG (`AC1xxx`), se commitea en la rama y se
   empuja al NAS. La rama pasa a "Para revisar".
5. **Integrar** — overlay de los archivos cambiados por la rama sobre `main` y
   commit de dos padres. Si `main` cambió alguno de esos archivos desde la base
   de la rama, hay conflicto: se revisa y se decide (integrar igual con
   `take-branch`, o descartar la rama).

### Reglas que conviene tener claras

- Un plano pertenece a **un** proyecto. El `drawingId` es
  `<proyecto>--<slug del nombre sin extensión>`.
- Una rama puede cubrir **varios planos**; una subida puede traer varios archivos.
- La metadata de la rama (título, autor, planos, notas) viaja **dentro** del
  commit en `.lcs/branch.json`. Cualquier máquina la puede leer sin la base local.
- `.lcs/**` nunca se mezcla a `main`: es metadata de la rama.
- Los `.dwf` no se importan (solo `.dwg`).

## Planos grandes (100-150 MB)

Los planos pesados son el caso normal acá, no la excepción. Qué hace LCS con ellos:

- **El visor no los abre.** Arriba de **32 MB** la vista previa se reemplaza por
  una tarjeta con descarga directa. Parsear 150 MB en WASM tarda minutos y puede
  quedarse sin memoria en el navegador; LCS prefiere decirte que lo abras en
  AutoCAD antes que colgar la pestaña.
- **Descarga y ZIP van en streaming.** Ni el DWG ni el paquete pasan por un
  buffer en memoria: se leen del repositorio y se escriben al socket de a
  bloques.
- **La subida se procesa en disco.** El multipart se vuelca a un temporal y el
  commit indexa desde ahí. Pico de RAM medido con un DWG de 150 MB: ~1,2 GB
  (subida) y ~1 GB (importación).

Medición real en esta máquina, disco local, plano de 150 MB incompresible:

| Operación | Tiempo | RAM pico |
|---|---|---|
| Importar | 9,2 s | ~1,0 GB |
| Descargar | 0,8 s | — |
| ZIP de la tarea | 0,8 s | — |
| Subir cambios | 9,5 s | ~1,2 GB |
| Integrar en main | 9,0 s | — |

En el NAS esos tiempos pasan a estar dominados por la red (150 MB de subida y de
bajada por operación).

**Costo en disco**: cada revisión guarda el DWG completo, comprimido por git. Un
plano de 150 MB con 10 revisiones ocupa ~1,5 GB. Por eso la ficha del proyecto
muestra el tamaño del repositorio y se pone en ámbar arriba de 20 GB. Si algún
proyecto llega ahí, conviene archivar ramas viejas o mover los planos históricos
a otro proyecto.

## El visor

- Motor: `@mlightcad/cad-simple-viewer` con LibreDWG compilado a WASM. Corre en el
  navegador; el DWG se sirve desde `/api/drawings/{id}/file?ref=<commit>`.
- Se oculta el chrome propio del visor (línea de comandos, toolbar, campana):
  LCS es de solo lectura.
- Los layouts detectados se guardan en el índice y se muestran en el panel
  derecho; hacen falta para revisar espacio papel sin abrir AutoCAD.
- Limitación conocida: la fidelidad depende de LibreDWG (xrefs, SHX raros y
  algunos DWG muy nuevos pueden no verse perfectos). El archivo original siempre
  se descarga intacto.

## Compilar

Requiere Go 1.24+ y Node 20+. Configura las rutas en `.env`; el build las
usa para encontrar Go, Node y npm, y el ejecutable las usa para cargar el
workspace y las rutas de datos. Puedes copiar `.env.example` como base.

```powershell
# Compila el frontend y el ejecutable Wails frameless
.\build.ps1
```

El primer `npm install` trae los workers de LibreDWG y las fuentes curadas
(`web/scripts/assets.mjs`); pesan ~19 MB y quedan dentro del `.exe`.

### Modo consola (desarrollo)

```powershell
go run -tags "desktop,production" ./cmd/lcs --headless -v  # servidor, logs a consola
go run -tags "desktop,production" ./cmd/lcs --open         # ventana y navegador
```

Flags: `--headless` (sin bandeja ni navegador), `--no-tray`, `--open`, `-v`.

## API

Todo es JSON bajo `/api`. Lo que usa la SPA:

| Método | Ruta | Qué hace |
|---|---|---|
| GET | `/api/bootstrap` | Configuración efectiva y contadores |
| GET | `/api/stats` | Totales del panel |
| GET | `/api/activity` | Feed de eventos |
| GET/POST | `/api/projects` | Listar / crear proyecto |
| GET | `/api/projects/{id}` | Proyecto con planos, ramas y eventos |
| POST | `/api/projects/{id}/sync` | Refrescar índice desde git |
| POST | `/api/projects/{id}/import` | Importar DWG (multipart) |
| GET/POST | `/api/projects/{id}/branches` | Listar / abrir rama |
| GET | `/api/drawings/{id}` | Plano + layouts + revisiones |
| GET | `/api/drawings/{id}/file?ref=` | DWG (de `main` o de una revisión) |
| PUT | `/api/drawings/{id}/layouts` | Persistir layouts detectados |
| POST | `/api/drawings/{id}/restore` | Restaurar revisión en `main` |
| GET | `/api/branches?status=` | Ramas de todos los proyectos |
| GET | `/api/branches/{id}` | Detalle con archivos y revisiones |
| POST | `/api/branches/{id}/upload` | Subir DWG editado (multipart) |
| GET | `/api/branches/{id}/download` | ZIP de la tarea |
| POST | `/api/branches/{id}/merge` | Integrar (`strategy: take-branch` opcional) |
| POST | `/api/branches/{id}/close` | Descartar rama |

## Decisiones de diseño

- **Backend stdlib** (`net/http` + `modernc.org/sqlite`), sin framework. El
  dominio es el flujo de ramas; un panel de administración genérico no aportaba.
- **go-git puro**: el `.exe` no depende de `git.exe` instalado ni de LFS. Los DWG
  se guardan como blobs normales (un plano de 2 MB por revisión: aceptable para
  el volumen del estudio; si crece, el siguiente paso es LFS o deduplicación).
- **Un bare repo por proyecto** + **un clon local por máquina**. El clon evita
  reescribir el árbol de trabajo en el NAS en cada operación.
- **Metadata en el commit**: `.lcs/branch.json` permite que otra máquina entienda
  la rama sin base de datos compartida.
- **Merge por overlay** en vez de merge de tres vías línea a línea: los DWG son
  binarios, así que lo único sensato es archivo completo por archivo, con
  detección de intersección de cambios.

## Riesgos conocidos

- LibreDWG es GPLv3: si el `.exe` se distribuye fuera del estudio, revisar la
  licencia del binario embebido (`@mlightcad/libredwg-converter`).
- Sin LFS, cada revisión de un DWG pesa completo en el repositorio. Con planos de
  150 MB, 10 revisiones son 1,5 GB. Es el punto donde conviene decidir entre
  archivar, subir planos por etapas (solo la parte que se edita) o migrar a LFS.
- DWG muy nuevos (AC1032/R2018) dependen del soporte de LibreDWG; el archivo
  siempre se puede descargar aunque el visor falle.
- El acceso concurrente al NAS asume locks en `<workspace>/.lcs/locks`; si el
  share no soporta locks fiables (SMB sí), conviene un solo operador por proyecto.
- Mientras el workspace esté en `D:\LCS` no hay respaldo compartido: si se pierde
  el disco, se pierde el historial. Para respaldar alcanza con copiar
  `D:\LCS\projects` (los repositorios son autocontenidos).
