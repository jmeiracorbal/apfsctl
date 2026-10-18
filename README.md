# devvolume

CLI de Go para gestionar, de forma declarativa y verificable, volúmenes APFS dedicados a repositorios en macOS.

**Estado: esqueleto inicial y operaciones de solo lectura.** No crea, monta, cifra ni elimina volúmenes todavía. Los comandos de modificación devuelven un error explícito para impedir cambios no validados.

## Requisitos

- macOS con APFS, `diskutil` y `plutil`.
- Go 1.22 o superior para compilar.

```sh
go test ./...
go build -o devvolume ./cmd/devvolume
./devvolume status
./devvolume plan --container <UUID-del-contenedor> --name repos --mount "$HOME/repos"
./devvolume doctor
```

`status` y `doctor` muestran un snapshot APFS obtenido de `diskutil`; `plan` calcula los cambios sobre el estado real, **sin aplicar ninguno**. El UUID del contenedor se obtiene con `status`. Todos los datos se procesan mediante plist/JSON estructurado. Nunca se ejecuta un intérprete Bash.

## Semántica de idempotencia

`internal/reconcile.Build` compara la configuración deseada con volúmenes APFS observados. Cuando el volumen ya existe, está cifrado y está montado en la ubicación exacta, devuelve `noop: true` y `changes: []`. Si está presente pero bloqueado, únicamente planifica desbloquearlo y montarlo. Si falta, planifica crearlo cifrado y montarlo. Nunca vuelve a planificar la creación de un volumen existente. Los conflictos de nombre, contenedor, cifrado o punto de montaje bloquean el procedimiento en lugar de realizar sustituciones implícitas.

Una ejecución fallida de `diskutil` o un estado incompleto son errores, **no** se interpretan como ausencia de volúmenes. Antes de planificar un montaje se comprueba que no se ocultarán archivos existentes en la ruta de destino.

## Alcance del primer hito

- Modelo Go del estado deseado y observado.
- Motor puro de reconciliación con pruebas de idempotencia y conflictos.
- Adaptador macOS de solo lectura basado en comandos con vectores de argumentos (`exec.CommandContext`).
- Lectura de plist con `plutil` y deserialización JSON tipada.
- CLI `status`, `doctor` y `plan`; comandos mutadores deshabilitados.
- CI para Linux (pruebas simuladas) y macOS.

**No implementado aún:** creación/cifrado APFS, desbloqueo, escritura segura de `/etc/fstab`, persistencia de montaje, control de Spotlight, FSEvents, recuperación de transacciones, copia de seguridad, ni eliminación. El `noop` actual se limita al cifrado y montaje del volumen; **no certifica** que esas otras políticas estén configuradas. No almacenar contraseñas en argumentos, logs ni archivos de estado.

## Organización

- `cmd/devvolume`: comandos y validación preventiva del directorio de montaje.
- `internal/domain`: tipos de dominio e invariantes.
- `internal/reconcile`: planificación determinista e idempotente.
- `internal/apfs`: descubrimiento estructurado del estado real de APFS.
- `internal/system`: frontera de ejecución de utilidades nativas de macOS.
