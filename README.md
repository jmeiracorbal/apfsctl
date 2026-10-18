# devvolume

CLI Go para gestionar volúmenes APFS cifrados para repositorios de desarrollo en macOS, con reconciliación idempotente y comprobaciones explícitas del estado real.

## Estado

**Hitos 3 y 4: aprovisionamiento y configuración experimentales.** Están implementados el descubrimiento, el diagnóstico y los comandos `create`, `mount` y `configure`. Todavía **no se han validado las operaciones privilegiadas sobre APFS real** ni el montaje tras reiniciar macOS. La primera prueba debe realizarse con un contenedor desechable y copia de seguridad.

Se implementa la edición idempotente de `fstab` y las políticas de Spotlight, FSEvents y propietario mediante `configure`. No están implementados `unmount` ni `remove`. El desbloqueo automático al arrancar tampoco está implementado; `fstab` únicamente conserva la configuración de montaje.

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
- `internal/mount`: reconciliación de `fstab` con `vifs`.
- `internal/policy`: políticas del volumen y permisos.
- `internal/system`: ejecución de comandos sin intérprete de shell.

No se ejecutan scripts Bash, `sh -c` ni comandos construidos concatenando argumentos. Todas las modificaciones usan una llamada a una utilidad nativa con lista explícita de argumentos.


## Paso 4: políticas del sistema (experimental)

El comando `configure` solo acepta un volumen APFS cifrado que ya esté montado exactamente en la ruta indicada y cuya identidad UUID se confirme antes de cada cambio:

```sh
# Se usa la ruta real del directorio y el UID del usuario, no el de root.
sudo ./devvolume configure --container <APFS-container-UUID> --name repos --mount /Users/usuario/repos --uid 501 --spotlight off --fsevents keep
```

- `fstab`: entrada UUID que conserva las demás líneas. La herramienta invoca `/usr/sbin/vifs` y utiliza el mismo binario Go como editor nativo; no genera scripts Bash. Si la entrada coincide exactamente, no edita el archivo.
- Spotlight: `mdutil -s` comprueba el estado antes de usar `mdutil -i off`. Un resultado de estado desconocido detiene el comando sin asumir éxito.
- FSEvents: `--fsevents keep` no modifica el historial ni sus marcadores. `--fsevents disable` añade explícitamente `.fseventsd/no_log`; puede afectar a las copias incrementales y no es reversible automáticamente.
- Permisos: `--uid 501` ajusta solo el propietario de la raíz del volumen, nunca recursivamente; `--uid -1` (valor por defecto) preserva los permisos actuales.

Repetir el comando cuando las políticas ya coinciden devuelve `noop: true`. La edición de `fstab` persiste **la ruta de montaje**, pero **no desbloquea automáticamente un APFS cifrado al reiniciar**; debe desbloquearse con `mount` u otro mecanismo autorizado. Esta implementación necesita pruebas de integración con `vifs` y reinicios reales de macOS antes de considerarse estable. No se presupone una cadena de montaje automático.

## Limitaciones importantes

El comando `plan` evalúa exclusivamente cifrado y montaje APFS. El comando `configure` reconcilia `fstab`, Spotlight, FSEvents (solo si se solicita) y propietario (solo si se especifica UID). No se comprueba todavía la política de copias de seguridad ni se automatiza el desbloqueo durante el arranque. Las operaciones de `configure` requieren validación en macOS real.

**No ejecutar en datos importantes sin revisar la salida de `plan` y disponer de una copia de seguridad.**
