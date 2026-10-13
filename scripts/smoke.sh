#!/bin/sh
# Smoke test for a running reference stack. It expects the compose ports.
set -eu

orders_url=${ORDERS_URL:-http://localhost:8081}
inventory_url=${INVENTORY_URL:-http://localhost:8082}
notifications_url=${NOTIFICATIONS_URL:-http://localhost:8083}
payments_url=${PAYMENTS_URL:-http://localhost:8086}
shipping_url=${SHIPPING_URL:-http://localhost:8087}
invoicing_url=${INVOICING_URL:-http://localhost:8088}
gateway_url=${GATEWAY_URL:-http://localhost:8090}

echo "inventory stock:"
curl -fsS "$inventory_url/stock/WIDGET-001"
echo

echo "create order:"
order=$(curl -fsS -X POST "$orders_url/orders" \
  -H 'Content-Type: application/json' \
  -d '{"customerRef":"smoke-test","lines":[{"sku":"WIDGET-001","quantity":2,"unitPriceMinor":1999}]}')
echo "$order"
order_id=$(printf '%s' "$order" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
provider_ref=$(printf '%s' "$order" | sed -n 's/.*"providerRef":"\([^"]*\)".*/\1/p')
[ -n "$order_id" ] || { echo "no order id in the response" >&2; exit 1; }
[ -n "$provider_ref" ] || { echo "no provider reference in the response" >&2; exit 1; }

echo "stock conflict status (expected 409):"
curl -sS -o /dev/null -w '%{http_code}\n' -X POST "$orders_url/orders" \
  -H 'Content-Type: application/json' \
  -d '{"customerRef":"smoke-test","lines":[{"sku":"WIDGET-001","quantity":99999,"unitPriceMinor":1999}]}'

echo "capture the payment:"
curl -fsS -X POST "$payments_url/webhooks/provider" \
  -H 'Content-Type: application/json' \
  -d "{\"providerRef\":\"$provider_ref\",\"status\":\"paid\"}"
echo

echo "waiting for the event chain:"
sleep 6

echo "order after payment:"
curl -fsS "$orders_url/orders/$order_id"
echo

echo "shipment:"
curl -fsS "$shipping_url/shipments/$order_id"
echo

echo "invoice:"
curl -fsS "$invoicing_url/invoices/$order_id"
echo

echo "notifications:"
curl -fsS "$notifications_url/notifications?limit=10"
echo

echo "gateway reservation (REST to internal gRPC):"
curl -fsS -X POST "$gateway_url/reservations" \
  -H 'Content-Type: application/json' \
  -d '{"items":[{"sku":"WIDGET-002","quantity":1}]}'
echo
