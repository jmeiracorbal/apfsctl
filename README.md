# devvolume

CLI Go para gestionar volúmenes APFS cifrados para repositorios de desarrollo en macOS, con reconciliación idempotente y comprobaciones explícitas del estado real.

## Estado

**Hito 3: aprovisionamiento experimental.** Están implementados el descubrimiento, el diagnóstico y los comandos `create` y `mount`. Todavía **no se ha validado el aprovisionamiento contra APFS real** en un Mac. Se recomienda probarlo primero en un contenedor APFS de pruebas con copia de seguridad.

No están implementados el montaje persistente (`fstab`), las políticas de Spotlight/FSEvents, ni los comandos `unmount` y `remove`. La titularidad de archivos del nuevo volumen tampoco se modifica automáticamente.

## Requisitos

- macOS con `diskutil` y `plutil`.
- Go 1.22 o superior.
- Permisos de administrador para operaciones de escritura.
- Terminal interactivo para las frases de acceso.
- Un directorio de montaje **ya existente, vacío y sin enlaces simbólicos**.

```sh
go test ./...
go vet ./...
go build -o devvolume ./cmd/devvolume

./devvolume status
./devvolume doctor
./devvolume plan --container <APFS-container-UUID> --name repos --mount "$HOME/repos"
```

El UUID del contenedor se consulta mediante `status`, no se calcula a partir del identificador `diskN`.

## Crear o recuperar el volumen

Preparar previamente el directorio como usuario normal:

```sh
mkdir -p "$HOME/repos"
```

Consultar el plan y verificar el UUID del contenedor **antes de escribir**. A continuación:

```sh
sudo ./devvolume create --container <APFS-container-UUID> --name repos --mount "$HOME/repos"
```

`create` **no duplica** un volumen existente compatible. Si falta, solicita la clave de cifrado mediante el diálogo interactivo de `diskutil` (`-passprompt`), crea el volumen APFS ya cifrado y verifica el resultado. Si existe y está bloqueado, solicita la clave al desbloquearlo. Los secretos nunca se transmiten como argumentos de línea de comandos, ni se guardan en el diario del programa.

El comando `mount` aplica solamente el desbloqueo/montaje de un volumen ya existente:

```sh
sudo ./devvolume mount --container <APFS-container-UUID> --name repos --mount "$HOME/repos"
```

El punto de montaje se debe indicar explícitamente al ejecutar con `sudo`, para no depender del directorio personal de `root`.

## Idempotencia y recuperación

El estado efectivo de APFS tiene prioridad sobre cualquier indicador local. Si nombre, contenedor, cifrado y montaje coinciden, la CLI devuelve `noop: true` **sin escribir ni solicitar privilegios**. Antes de cada mutación, el plan se recalcula bajo un bloqueo exclusivo y se valida otra vez el directorio; después se comprueba su efecto con una nueva lectura de APFS.

Se guarda un diario transaccional, sin contraseñas, en `/private/var/db/devvolume`, dentro de un directorio privado propiedad de `root`. Cada operación se registra y se verifica. Si la creación quedó interrumpida y no se puede determinar su resultado, el comando se detiene para inspección manual: **nunca reintenta una creación ambigua**. No realiza rollback destructivo.

No se adoptan volúmenes con roles de sistema; los conflictos de nombres, UUID, cifrado o punto de montaje detienen la operación.

## Arquitectura

- `cmd/devvolume`: CLI y comprobaciones de rutas.
- `internal/domain`: modelos, validación e identidades.
- `internal/reconcile`: cálculo puro de cambios (sin efectos secundarios).
- `internal/diagnostic`: diagnósticos y detección de drift.
- `internal/apfs`: adaptador estructurado para `diskutil`.
- `internal/provision`: orquestación y verificación de operaciones.
- `internal/state`: diario atómico y bloqueo entre procesos.
- `internal/system`: ejecución de comandos sin intérprete de shell.

No se ejecutan scripts Bash, `sh -c` ni comandos construidos concatenando argumentos. Todas las modificaciones usan una llamada a una utilidad nativa con lista explícita de argumentos.

## Limitaciones importantes

El cifrado y el montaje APFS se reconcilian en el paso 3; **la configuración completa del sistema no**. El resultado `noop` no comprueba aún `fstab`, Spotlight, FSEvents, propiedad del directorio raíz del nuevo volumen ni política de copias de seguridad. La gestión de esas políticas pertenece al paso 4.

**No ejecutar en datos importantes sin revisar la salida de `plan` y disponer de una copia de seguridad.**
