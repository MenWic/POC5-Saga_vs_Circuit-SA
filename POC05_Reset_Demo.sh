#!/usr/bin/env bash
# Reinicio EXCLUSIVO de las cuatro bases locales de POC05
# Ejecutar solo con los cuatro microservicios detenidos
set -euo pipefail

export PATH="/Library/PostgreSQL/18/bin:$PATH"
PG_HOST='127.0.0.1'
PG_PORT='5432'
PG_USER='postgres'
DBS=(poc05_orders poc05_payments poc05_inventory poc05_shipping)
TABLES=(orders payments reservations shipments)

command -v psql >/dev/null || { echo 'ERROR: no se encontró psql'; exit 1; }
command -v pg_isready >/dev/null || { echo 'ERROR: no se encontró pg_isready'; exit 1; }
pg_isready -h "$PG_HOST" -p "$PG_PORT" >/dev/null || { echo 'ERROR: PostgreSQL no acepta conexiones'; exit 1; }

if command -v nc >/dev/null 2>&1; then
  for port in 8080 8081 8082 8083; do
    if nc -z "$PG_HOST" "$port" >/dev/null 2>&1; then
      echo "ERROR: el puerto $port sigue activo. Detené los cuatro servicios antes de reiniciar"
      exit 1
    fi
  done
fi

# Comprobar las cuatro bases y tablas ANTES de tocar cualquier dato
for n in 0 1 2 3; do
  db="${DBS[$n]}"
  table="${TABLES[$n]}"
  exists="$(psql -X -qAt -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d "$db" -c "SELECT to_regclass('public.$table') IS NOT NULL;")"
  [[ "$exists" == 't' ]] || { echo "ERROR: falta public.$table en $db"; exit 1; }
done
exists="$(psql -X -qAt -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d poc05_inventory -c "SELECT to_regclass('public.items') IS NOT NULL;")"
[[ "$exists" == 't' ]] || { echo 'ERROR: falta public.items en poc05_inventory'; exit 1; }

echo 'Se eliminarán SOLO los datos de las cuatro bases poc05_* y se reiniciarán sus IDs'
read -r -p 'Escribí REINICIAR para confirmar: ' confirmation
[[ "$confirmation" == 'REINICIAR' ]] || { echo 'Cancelado'; exit 1; }

psql -X -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d poc05_orders \
  -c 'TRUNCATE TABLE public.orders RESTART IDENTITY;'
psql -X -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d poc05_payments \
  -c 'TRUNCATE TABLE public.payments RESTART IDENTITY;'
psql -X -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d poc05_inventory \
  -c "BEGIN; TRUNCATE TABLE public.reservations, public.items RESTART IDENTITY; INSERT INTO public.items (sku, available_quantity) VALUES ('SKU-001', 100); COMMIT;"
psql -X -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d poc05_shipping \
  -c 'TRUNCATE TABLE public.shipments RESTART IDENTITY;'

echo 'Reinicio completado. Stock actual:'
psql -X -v ON_ERROR_STOP=1 -h "$PG_HOST" -p "$PG_PORT" -U "$PG_USER" -d poc05_inventory \
  -c 'SELECT sku, available_quantity FROM public.items;'
echo 'Iniciá nuevamente los cuatro microservicios. Los circuitos se reinician al arrancar Order'
