# Saga Pattern vs Circuit Breaker Pattern

## Descripción General

En esta práctica se debe implementar un PoC que demuestre el uso de dos patrones fundamentales en arquitecturas de microservicios:

- Saga Pattern: Para gestionar transacciones distribuidas y garantizar la consistencia eventual entre servicios.
- Circuit Breaker Pattern: Para prevenir fallos en cascada y mejorar la resiliencia del sistema ante dependencias fallidas.

El objetivo es que el estudiante comprenda cuándo, cómo y por qué aplicar estos patrones, que sea capaz de implementarlos con su stack tecnológico preferido, y que analice críticamente sus ventajas, desventajas y contextos de aplicación.

---

## Objetivos

- Diseñar una arquitectura de microservicios con transacciones distribuidas.
- Implementar una Saga (coreografía u orquestación) con compensaciones.
- Implementar un Circuit Breaker con sus tres estados (Closed, Open, Half-Open).
- Simular fallos y verificar el comportamiento resiliente del sistema.
- Analizar pros, contras y escenarios de aplicación de cada patrón.
- Comunicar los resultados mediante un video demostrativo y un informe técnico.

---

## Escenario de Negocio

Se debe implementar un flujo de compra en un e-commerce compuesto por 4 microservicios:

```
┌─────────────┐    ┌──────────────┐    ┌───────────────┐    ┌─────────────┐
│   Order     │──▶│   Payment    │──▶│   Inventory   │──▶│  Shipping   │
│  Service    │    │   Service    │    │   Service     │    │  Service    │
└─────────────┘    └──────────────┘    └───────────────┘    └─────────────┘
```

**Flujo exitoso:**

1. **Order Service:** crea la orden.
2. **Payment Service:** cobra al cliente.
3. **Inventory Service:** descuenta stock.
4. **Shipping Service:** programa el envío.

**Flujo con fallo (ejemplo):** Si el **Inventory Service** falla (sin stock), el sistema debe:

- Revertir el cobro (compensación en Payment).
- Cancelar la orden (compensación en Order).

---

## Desarrollo de la practica: Investigación y Diseño

### 1. Saga Pattern

**Problema:** En microservicios, cada servicio tiene su propia base de datos, por lo que no se pueden usar transacciones ACID distribuidas tradicionales (2PC es costoso y bloqueante).

**Dos enfoques:**
- Coreografía
- Orquestación

**Compensaciones**

---

### 2. Circuit Breaker Pattern

**Problema:** Si un servicio dependiente falla, las llamadas repetidas pueden saturar el sistema y provocar fallos en cascada.


**Tres estados:**

- **Closed:**
- **Open:**
- **Half-Open:**

**Configuraciones comunes:**

- `failureRateThreshold`: % de fallos para abrir (ej. 50%).
- `slowCallRateThreshold`: % de llamadas lentas.
- `waitDurationInOpenState`: tiempo antes de pasar a Half-Open.
- `slidingWindowSize`: número de llamadas a considerar.
- `permittedNumberOfCallsInHalfOpenState`: llamadas de prueba.

**Patrones complementarios:**

- **Retry:** reintentar operaciones fallidas.
- **Timeout:** limitar el tiempo de espera.
- **Bulkhead:** aislar recursos por servicio.
- **Fallback:** respuesta alternativa cuando falla.

---

## Desarrollo de la practica: Implementacion de microservicios

1. **Implementar los 4 microservicios** con sus endpoints básicos:
   - `POST /orders` (Order)
   - `POST /payments` (Payment)
   - `POST /inventory/reserve` (Inventory)
   - `POST /shipping/schedule` (Shipping)
2. **Configurar** la comunicación entre servicios (REST, gRPC o mensajería).
3. **Verificar** que cada servicio funciona individualmente.

### Implementación de la Saga

1. **Implementar la Saga** según el enfoque elegido: Coreografía u Orquestación

2. **Implementar las transacciones compensatorias:**
   - `DELETE /orders/{id}`
   - `POST /payments/{id}/refund`
   - `POST /inventory/release`
   - `DELETE /shipping/{id}`

3. **Probar el flujo feliz** (todos los pasos exitosos).

### Implementación del Circuit Breaker

1. **Integrar la librería de Circuit Breaker** en las llamadas entre servicios.

2. **Configurar** al menos **dos Circuit Breakers**

3. **Configurar parámetros:**

   ```
   failureRateThreshold = 50%
   waitDurationInOpenState = 10s
   slidingWindowSize = 10
   permittedNumberOfCallsInHalfOpenState = 3
   ```

4. **Implementar fallbacks:**
   - Cuando el Circuit Breaker esté abierto, devolver una respuesta controlada (ej. "Servicio no disponible, intente más tarde").
   - Opcional: encolar la petición para reintentar después.

5. **Probar el flujo feliz** con el Circuit Breaker cerrado.

### Actividad 5: Pruebas de Fallos

1. **Simular fallos y verificar la Saga**

2. **Simular fallos y verificar el Circuit Breaker**

3. **Incluir video demostrativo de las pruebas de fallos**