# Use cases — dictado con Grok (xAI)

Interfaz: **CLI** (`cmd/stt-probe`). La UI de Ajustes es todavía un stub (fase 4), así que no
hay recorrido de UI que ejecutar: el selector de proveedor se ve en `frontend/index.html:769`
pero no responde a nada. El recorrido por la app (tecla `fn` → pegado → historial) se cubrirá
cuando exista la fase 4 y haya una key.

---

## UC-GROK-01 — llego nuevo y todavía no he puesto mi API key

- **Actor:** alguien que acaba de elegir Grok en Ajustes y aún no ha pegado su key.
- **Escenario:** intenta dictar sin credencial, y luego con una.
- **Interfaz:** CLI
- **Intención:** que la app diga **qué** falta y **dónde** arreglarlo, en vez de fallar de una
  forma que parezca un problema de red o de micrófono.
- **Setup:** ninguno. No configurar ninguna key (el estado de un usuario nuevo es el estado por
  defecto — el Setup no puede realizar la acción bajo prueba).
- **Pasos:**
  1. `env -u XAI_API_KEY ./scripts/go.sh run ./cmd/stt-probe -provider grok -seconds 1`
  2. Repetir **con** una key presente, para comprobar que el resultado del paso 1 venía de leer
     la credencial y no de fallar siempre igual.
- **Verificación:** el paso 1 reporta `NotConfigured` con un mensaje que nombra Ajustes; el
  paso 2 llega **más allá** de la configuración y falla en la autenticación. Dos resultados
  distintos ⇒ la lectura de la key es real.
- **Persistencia:** el código emitido, clasificado por la política de reintentos de
  `internal/session`, debe dar `reconnect=false`: un dictado que no puede funcionar no debe
  quedar reintentando contra un servicio que factura por hora.

---

## UC-GROK-02 — me equivoqué al pegar la key

- **Actor:** alguien que pegó una key mal copiada.
- **Escenario:** dicta con una credencial que el servicio real rechaza.
- **Interfaz:** CLI
- **Intención:** que le digan **que es la key**, y que la app no se quede reintentando.
- **Setup:** exportar `XAI_API_KEY` con un valor inválido. Contra el servicio **real**, que es
  el único que puede decir cómo rechaza de verdad.
- **Pasos:**
  1. `XAI_API_KEY=<inválida> ./scripts/go.sh run ./cmd/stt-probe -provider grok -seconds 2`
  2. Clasificar el código emitido con `session.ClassifyCancel` + `session.ShouldReconnect`.
- **Verificación:** un solo `canceled` cuyo mensaje nombra la API key; **no** un "status 400"
  genérico, que mandaría al usuario a auditar su configuración.
- **Persistencia:** `reconnect=false`.

---

## UC-GROK-03 — dicto dos frases y aparecen como un solo mensaje *(BLOQUEADO)*

- **Actor:** alguien con una cuenta de xAI válida.
- **Escenario:** mantiene la tecla, dice dos frases con una pausa clara, suelta.
- **Interfaz:** CLI (y luego la app)
- **Intención:** que las dos frases lleguen **unidas en un mensaje**, en orden y **sin
  duplicados** — el fallo que el mapeo de eventos de Electron produciría.
- **Setup:** `XAI_API_KEY` con una key válida.
- **Pasos:**
  1. `XAI_API_KEY=<válida> ./scripts/go.sh run ./cmd/stt-probe -provider grok -seconds 20`,
     hablando dos frases con una pausa clara.
  2. Registrar **cada evento literal** y comparar con la línea de tiempo ensamblada.
- **Verificación:** un solo `FINAL` con las dos frases, en orden, sin ninguna repetida.
- **Persistencia:** repetirlo por la app y comprobar que `history.jsonl` gana **un** registro.
- **BLOQUEADO:** no hay key de xAI. Es el mismo bloqueo que tiene Azure (ver `CONTINUITY.md`).
  Este recorrido es además el experimento que confirma el riesgo 1 del plan (si `start` es
  relativo a la sesión); hay que ejecutarlo en cuanto haya credencial.
