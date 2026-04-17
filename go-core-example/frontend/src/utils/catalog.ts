export const currency = new Intl.NumberFormat("en-US", {
  style: "currency",
  currency: "USD",
});

export function formatPrice(price?: number): string {
  return currency.format(price ?? 0);
}

export function stockLabel(stock?: number): string {
  if (!stock || stock <= 0) {
    return "out of stock";
  }
  if (stock <= 10) {
    return `${stock} left`;
  }
  return `${stock} in stock`;
}

export function stockClass(stock?: number): string {
  if (!stock || stock <= 0) {
    return "stock stock-empty";
  }
  if (stock <= 10) {
    return "stock stock-low";
  }
  return "stock stock-ok";
}
