-- Ejecutar UNA SOLA VEZ conectado a la base administrativa 'postgres'.
-- Requiere un usuario con permiso CREATEDB y autocommit activado.
-- Cada sentencia CREATE DATABASE debe ejecutarse fuera de una transacción.
CREATE DATABASE poc05_orders;
CREATE DATABASE poc05_payments;
CREATE DATABASE poc05_inventory;
CREATE DATABASE poc05_shipping;
