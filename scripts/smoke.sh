#!/bin/sh
# Smoke test for a running reference stack. It expects the compose ports.
set -eu

orders_url=${ORDERS_URL:-http://localhost:8081}
inventory_url=${INVENTORY_URL:-http://localhost:8082}
notifications_url=${NOTIFICATIONS_URL:-http://localhost:8083}

echo "inventory stock:"
curl -fsS "$inventory_url/stock/WIDGET-001"
echo

echo "create order:"
order=$(curl -fsS -X POST "$orders_url/orders" \
  -H 'Content-Type: application/json' \
  -d '{"customerRef":"smoke-test","lines":[{"sku":"WIDGET-001","quantity":2,"unitPriceMinor":1999}]}')
echo "$order"
order_id=$(printf '%s' "$order" | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')
[ -n "$order_id" ] || { echo "no order id in the response" >&2; exit 1; }

echo "read order:"
curl -fsS "$orders_url/orders/$order_id"
echo

echo "stock conflict status (expected 409):"
curl -sS -o /dev/null -w '%{http_code}\n' -X POST "$orders_url/orders" \
  -H 'Content-Type: application/json' \
  -d '{"customerRef":"smoke-test","lines":[{"sku":"WIDGET-001","quantity":99999,"unitPriceMinor":1999}]}'

echo "waiting for the outbox relay:"
sleep 3

echo "notifications:"
curl -fsS "$notifications_url/notifications?limit=5"
echo
