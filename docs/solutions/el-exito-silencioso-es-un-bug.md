# El éxito silencioso es un bug (y borró una credencial)

## Problema

El usuario, vinculando Azure desde Ajustes → Conexiones: "al colocar la key y darle en probar
conexión, usar motor, borrar clave y guardar no funciona ninguna acción". Más tarde, con más
detalle: "usar este motor el feedback también es raro, solo aparece por una fracción de tiempo muy
rápida"; "guardar más de lo mismo, un `...` muy rápido que ni lo alcanzo a ver".

## Causa raíz — cuatro, no una

Los logs de la app (`UI-ACTION`) demostraron que **dos de los cuatro botones sí llamaron al backend
y ambas llamadas tuvieron éxito**. El síntoma era único; las causas, cuatro:

1. **"Probar conexión" nunca se portó.** `#test` sin handler y sin método bindeado, aunque
   `azure.TestConnection` existía y estaba unit-testeada desde el port. Código muerto desde el lado
   del usuario.
2. **Un botón deshabilitado se veía igual que uno activo.** No existía ninguna regla
   `button.btn:disabled`, y `button.btn` fija color, fondo y hover.
3. **El éxito no se decía.** `run()` pintaba `WriteResult.Error`, que en éxito es cadena vacía: el
   `…` de "en curso" se sustituía por nada.
4. **Un éxito podía no cambiar nada visible.** Elegir Azure sin clave se guarda correctamente, pero
   el estado sigue siendo `unconfigured`, así que la insignia y el botón no se movían.

## Lo que lo convierte en algo más que estética

Con una clave guardada, el usuario pulsó **"Borrar clave"**. La llamada corrió, tuvo éxito, no dijo
nada — y el ítem `azure-speech` desapareció del Keychain. **Una acción destructiva que se completa
en silencio es indistinguible de un botón roto**, y la diferencia solo se descubre cuando hace falta
la credencial. El síntoma reportado era "no funciona"; el hecho era "funcionó y no te avisó".

## Solución

- El resultado de un setter lleva **`Notice`** además de `Error`, y la página pinta `✓ notice` o
  `✗ error` con las clases que la CSS ya tenía. El texto lo decide Go porque depende de hechos que
  solo Go tiene: qué se escribió realmente, en qué estado quedó el motor, qué contestó Azure.
- El texto de "en curso" **nombra la actividad** ("Guardando…", "Probando la conexión…") en vez de
  un `…` que se lee como parpadeo.
- **Postcondición, no narración**, cuando la acción es idempotente: `store.DeleteKey` trata "no
  había nada" como éxito, así que el mensaje es "La clave ya no está guardada" y no "Clave borrada",
  que sería falso justo cuando el usuario pulsa sobre un slot vacío.
- **`Field`** en el resultado nombra el input a señalar; la página pone la clase y no deduce nada.

## Prevención — las cinco reglas que este bug deja

1. **Una acción sin resultado observable no está terminada.** Vale para las cuatro: la que no existe,
   la que está apagada sin parecerlo, la que acierta en silencio y la que acierta sin cambiar nada.
2. **Si es destructiva, el silencio es un fallo de seguridad, no de UX.**
3. **Un mensaje debe describir el estado final, no la acción**, siempre que la operación sea
   idempotente o su alcance dependa de lo que había.
4. **Los tres estados de una credencial no se colapsan.** "No hay clave", "el Keychain no respondió"
   y "la variable de entorno está en blanco" mandan al usuario a tres sitios distintos. Este cambio
   los confundió en el plan **tres veces seguidas** y las tres las cazó la revisión cruzada.
5. **Lo que se prueba tiene que ser lo que se guarda, byte a byte.** El probe recortaba el secreto y
   el guardado no: `" clave "` daba verde y se persistía con espacios. Un tick verde sobre una
   credencial distinta de la almacenada es peor que no tener botón de prueba.

## Lo que encontró el proceso, no el código

- **Seis iteraciones de revisión de plan** antes de escribir una línea. Cuatro fallos de diseño
  míos, uno de ellos habría cambiado un bug de "no me dice nada" por uno de "me miente": mi
  arbitraje descartaba el repintado autoritativo de un Guardar, dejando la clave guardada y el card
  diciendo lo contrario.
- **15 mutaciones de producción** contra los tests nuevos. **Dos pasaron en verde** y destaparon
  tests vacuos míos: uno afirmaba un orden que no comprobaba (pasaba una clave escrita, que corta el
  camino antes de llegar al Keychain), y otro arreglo se quedó sin test hasta que la mutación lo
  señaló. `CONTINUITY.md` ya llevaba esta lección de la sesión anterior; sigue valiendo.
- **Un `tsc` moderno de un solo uso** sobre un proyecto que no comprueba tipos encontró
  `slot.status === "configured"` en el tutorial: `KeyStatus` vale `present|absent|unreadable`, así
  que la rama estaba muerta y a quien ya tenía clave se le pedía pegar una.
