-- Conectar a la base poc05_inventory antes de ejecutar.
CREATE TABLE IF NOT EXISTS items (
    sku TEXT PRIMARY KEY,
    available_quantity INTEGER NOT NULL CHECK (available_quantity >= 0)
);

-- Una reserva por orden. La aplicación debe reservar y liberar mediante
-- transacciones locales para actualizar la reserva y el stock conjuntamente.
CREATE TABLE IF NOT EXISTS reservations (
    order_id BIGINT PRIMARY KEY,
    sku TEXT NOT NULL REFERENCES items(sku),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status TEXT NOT NULL DEFAULT 'RESERVED'
        CHECK (status IN ('RESERVED', 'RELEASED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    released_at TIMESTAMPTZ
);

-- Seed inicial seguro al repetir el script: no reinicia stock ya consumido.
INSERT INTO items (sku, available_quantity)
VALUES ('SKU-001', 100)
ON CONFLICT (sku) DO NOTHING;
