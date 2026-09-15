class Cart:
    def __init__(self, items=None):
        self.items = items if items is not None else []

    def add(self, item):
        self.items.append(item)
        return self.items


def apply_discounts(cart, discounts=[]):
    """Áp danh sách mã giảm giá vào cart, trả về danh sách đã áp dụng."""
    for code in discounts:
        cart.items = [i for i in cart.items if i.get("code") != code]
    discounts.append("applied")
    return discounts
