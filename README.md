# POC05: Saga y Circuit Breaker en Go

Prueba de concepto de una compra distribuida en **cuatro microservicios REST**. **Saga** coordina Order → Payment → Inventory → Shipping y compensa operaciones si falla un paso. Dos **Circuit Breakers** independientes protegen el cobro y la programación de envío ante fallos técnicos repetidos.

## Arquitectura y requisitos

| Servicio | Puerto | PostgreSQL | Responsabilidad |
| --- | --- | --- | --- |
| Order | `8080` | `poc05_orders` | API de órdenes, orquestación Saga y breakers |
| Payment | `8081` | `poc05_payments` | Cobros simulados y refunds |
| Inventory | `8082` | `poc05_inventory` | Reservas y liberación de stock |
| Shipping | `8083` | `poc05_shipping` | Envíos y cancelación |

Cada servicio tiene **su propio módulo Go**, proceso, base y API HTTP/JSON. No existen consultas SQL entre servicios: se comunican mediante REST.

**Requisitos:** Go **1.26.5** (declarado en los .mod), PostgreSQL, `psql` o DataGrip y puertos 8080–8083 libres. Librerías: `pgx/v5 v5.11.0` (los cuatro servicios) y `failsafe-go v0.9.8` (solo Order). La aplicación no requiere Docker, mensajería ni frontend.

## Preparar bases de datos

Usar una cuenta PostgreSQL local con permiso para crear bases. Ejecutar el primero **solo si las cuatro bases no existen**; los demás scripts sobre su base correspondiente:

```bash
psql -h 127.0.0.1 -U postgres -d postgres -f database/00_create_databases.sql
psql -h 127.0.0.1 -U postgres -d poc05_orders -f database/01_orders.sql
psql -h 127.0.0.1 -U postgres -d poc05_payments -f database/02_payments.sql
psql -h 127.0.0.1 -U postgres -d poc05_inventory -f database/03_inventory.sql
psql -h 127.0.0.1 -U postgres -d poc05_shipping -f database/04_shipping.sql
```

`00_create_databases.sql` requiere autocommit (no ejecutar dentro de transacción). El seed agrega `SKU-001` con stock 100 **solo si no existe**; no reinicia stock consumido. Autenticarse mediante `.pgpass` local o el gestor de credenciales, nunca incluir contraseñas en el repositorio.

## Iniciar cuatro servicios

Abrir **cuatro terminales** desde la raíz del repositorio, ejecutar un bloque por terminal, dejando los procesos abiertos. Orden recomendado: Payment, Inventory, Shipping y finalmente Order. No se cargan archivos `.env` automáticamente.

**Payment (:8081)**

```bash
cd services/payment
export PORT=8081 DATABASE_URL='postgres://postgres@127.0.0.1:5432/poc05_payments?sslmode=disable' DEMO_MODE=true
go run ./cmd
```

**Inventory (:8082)**

```bash
cd services/inventory
export PORT=8082 DATABASE_URL='postgres://postgres@127.0.0.1:5432/poc05_inventory?sslmode=disable'
go run ./cmd
```

**Shipping (:8083)**

```bash
cd services/shipping
export PORT=8083 DATABASE_URL='postgres://postgres@127.0.0.1:5432/poc05_shipping?sslmode=disable' DEMO_MODE=true
go run ./cmd
```

**Order (:8080)**

```bash
cd services/order
export PORT=8080 DATABASE_URL='postgres://postgres@127.0.0.1:5432/poc05_orders?sslmode=disable'
export PAYMENT_URL='http://127.0.0.1:8081' INVENTORY_URL='http://127.0.0.1:8082' SHIPPING_URL='http://127.0.0.1:8083'
go run ./cmd
```

Verificar: `curl http://127.0.0.1:8080/health` (también puertos 8081–8083). El endpoint `GET http://127.0.0.1:8080/demo/circuit-breakers` muestra los estados en memoria.

## Flujos HTTP y qué consultar en PostgreSQL

Abrir DataGrip con cuatro conexiones (o consolas), una por base. Las consultas permiten ver el **antes** y refrescar el **después** de cada caso:

```sql
-- poc05_orders
SELECT id, status, quantity, payment_id, shipping_id FROM orders ORDER BY id DESC LIMIT 20;
-- poc05_payments
SELECT id, order_id, status FROM payments ORDER BY id DESC LIMIT 20;
-- poc05_inventory
SELECT sku, available_quantity FROM items;
SELECT order_id, quantity, status FROM reservations ORDER BY order_id DESC LIMIT 20;
-- poc05_shipping
SELECT id, order_id, status FROM shipments ORDER BY id DESC LIMIT 20;
```

### Saga: una solicitud por escenario

| Flujo | Petición / preparación | Tablas para antes y después | Resultado |
| --- | --- | --- | --- |
| S1 Compra exitosa | `POST :8080/orders`, `{"sku":"SKU-001","quantity":2}` | Las cuatro | `201`, `COMPLETED`, cobro `CHARGED`, reserva `RESERVED`, envío `SCHEDULED`, stock −2 |
| S2 Inventario insuficiente | `POST :8080/orders`, `{"sku":"SKU-001","quantity":999}` | Orders, Payments, Inventory | `409`, `CANCELLED`, pago `REFUNDED`, stock sin cambio |
| S3 Payment fallido | `PUT :8081/demo/failure` ON → `POST :8080/orders` → OFF | Orders, Payments, Inventory | `503`, orden cancelada, sin nuevo cobro |
| S4 Shipping fallido | `PUT :8083/demo/failure` ON → `POST :8080/orders` → OFF | Las cuatro | `503`, pago `REFUNDED`, reserva `RELEASED`, stock restaurado |

Para enviar compras por Terminal:

```bash
curl -i -X POST http://127.0.0.1:8080/orders -H 'Content-Type: application/json' -d '{"sku":"SKU-001","quantity":2}'
```

Cambiar `quantity` de acuerdo al escenario que se quiera. Para activar/desactivar fallas:

```bash
curl -sS -X PUT http://127.0.0.1:8081/demo/failure -H 'Content-Type: application/json' -d '{"enabled":true}'
# Repetir con enabled:false; cambiar puerto a 8083 para Shipping
```

### Circuit Breaker: dos baterías independientes

Para **cada** batería, primero desactivar ambos fallos y **reiniciar solo Order** (restablece breakers a Closed, sin borrar registros PostgreSQL). Verificar el stock disponible y consultar `GET :8080/demo/circuit-breakers`.

| Paso | Payment (`8081`) o Shipping (`8083`) | Qué observar |
| --- | --- | --- |
| C1 | Enviar **5 compras correctas** (`POST :8080/orders`, cantidad 1) | `201`; `executions=5`, `failures=0` del breaker elegido; cinco compras persistidas |
| C2 | Activar `PUT /demo/failure`, enviar **5 compras fallidas** y **de inmediato una sexta** | Cinco `503 ..._unavailable`, después `503 ..._circuit_open`; breaker `open`, `executions=10`, `failures=5`. La sexta no contacta la dependencia protegida |
| C3 | Desactivar fallo, esperar **11 s**, enviar **3 compras válidas** consultando diagnóstico entre ellas | Primera/segunda `half-open` (1/3, 2/3), tercera `closed`; tres nuevas compras completadas |

**En Shipping Open:** se realizan cobro y reserva antes del rechazo, por lo que se ejecutan `Refund` y `Release`. VSe ve en DataGrip `REFUNDED`, `RELEASED` y stock sin pérdida. **En Payment Open:** no se crea cobro; la orden se cancela. Los estados del breaker están en **memoria**, no en PostgreSQL. Los logs de Order también muestran sus transiciones.

**No detenerse entre la quinta falla y la sexta petición**: el estado Open dura 10 s. Si se espera, esa llamada podría pasar a Half-Open y dejar de demostrar el rechazo Open.

**Colección importable:** [`postman/POC05_Saga_CircuitBreaker.postman_collection.json`](postman/POC05_Saga_CircuitBreaker.postman_collection.json), con carpetas ordenadas por patrón. En Postman, las solicitudes «ENVIAR 5 VECES» se envían cinco veces manualmente.

## Restablecimiento manual (solo bases de demostración)

**Opcional:** para reiniciar a stock 100 y tablas vacías, detener primero los cuatro servicios; Estos comandos **eliminan registros anteriores** y no se ejecutan automáticamente:

```bash
psql -h 127.0.0.1 -U postgres -d poc05_orders -c 'TRUNCATE TABLE orders RESTART IDENTITY;'
psql -h 127.0.0.1 -U postgres -d poc05_payments -c 'TRUNCATE TABLE payments RESTART IDENTITY;'
psql -h 127.0.0.1 -U postgres -d poc05_inventory -c "TRUNCATE TABLE reservations; UPDATE items SET available_quantity=100 WHERE sku='SKU-001';"
psql -h 127.0.0.1 -U postgres -d poc05_shipping -c 'TRUNCATE TABLE shipments RESTART IDENTITY;'
```

Iniciar otra vez los cuatro procesos reinicia el estado en memoria de los breakers y las banderas demo.

## Pruebas y límites

```bash
for service in order payment inventory shipping; do
  (cd "services/$service" && go vet ./... && go test ./... && go build ./...)
done
```

Para ejecutar el test de integración de Inventory sin usar caché:

```bash
(cd services/inventory && INVENTORY_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:5432/poc05_inventory?sslmode=disable' go test ./internal/inventory -run TestPostgresReserveReleaseAtomicity -count=1 -v)
```

Se emplean cobros simulados y compensaciones locales. No se incluyen mensajería, reintentos automáticos, coordinación ACID global ni recuperación de resultados remotos inciertos.