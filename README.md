# devvolume

CLI Go declarativa para diagnosticar y, en futuros hitos, gestionar volúmenes APFS dedicados a repositorios en macOS.

**Estado: lectura y planificación.** Ningún comando modifica el almacenamiento. Las operaciones privilegiadas están deshabilitadas explícitamente.

## Requisitos

- macOS con `diskutil` y `plutil`.
- Go 1.22 o posterior.

```sh
go test ./...
go vet ./...
go build -o devvolume ./cmd/devvolume

./devvolume status
./devvolume doctor
./devvolume doctor --container <UUID-del-contenedor> --name repos --mount "$HOME/repos"
./devvolume plan --container <UUID-del-contenedor> --name repos --mount "$HOME/repos"
```

`status` devuelve contenedores y volúmenes APFS, incluidos UUID, referencia del contenedor (`diskN`), cifrado, roles y montaje. `doctor` realiza diagnósticos estructurados. Si se indica `--container`, además compara el estado observado con la configuración deseada; una desviación o conflicto termina con código distinto de cero. `plan` muestra los cambios necesarios sin ejecutarlos.

La salida de `diskutil` se lee como plist mediante `plutil` y se procesa como JSON tipado. No se usa Bash, `sh -c` ni scripts generados.

## Idempotencia

`internal/reconcile.Build` es un planificador puro. Un volumen existente, cifrado y montado exactamente en la ruta solicitada produce `noop: true` con `changes: []`. Si existe pero está bloqueado, planifica desbloqueo y montaje sin volver a crearlo. Si no existe, planifica creación cifrada y montaje. Cualquier observación ambigua, cifrado ausente, colisión de nombre o montaje en conflicto aborta sin aplicar cambios.

La validación usa identidad del contenedor APFS por UUID y su referencia real; nunca infiere referencias de disco. Los volúmenes de sistema con roles APFS están protegidos frente a adopción accidental. Antes de planificar un montaje, la CLI comprueba que el directorio esté vacío y que la ruta no atraviese enlaces simbólicos.

## Organización

- `cmd/devvolume`: CLI y comprobaciones del directorio de montaje.
- `internal/domain`: configuración deseada y snapshot APFS tipado.
- `internal/reconcile`: motor de idempotencia y conflictos.
- `internal/diagnostic`: informe de salud y desviaciones.
- `internal/apfs`: lectura de datos reales desde `diskutil`.
- `internal/system`: ejecución de comandos nativos mediante vectores de argumentos.

## Seguridad y limitaciones

Los comandos `create`, `mount`, `unmount` y `remove` todavía devuelven un error deliberado. El estado `noop` **solo verifica cifrado y montaje APFS**, no la configuración completa del sistema.

**Pendiente:** cifrado y creación de volúmenes reales, desbloqueo, gestión persistente de `fstab`, políticas de Spotlight y FSEvents, reconciliación de esas políticas, copia de seguridad, rollback y operaciones destructivas seguras. Nada de ello se considera implementado por las pruebas actuales.

El diagnóstico no sustituye las pruebas de integración en hardware macOS real. No almacenar contraseñas en argumentos, registros ni archivos de estado.
