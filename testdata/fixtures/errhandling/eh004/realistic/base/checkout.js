function submitOrder(order) {
	return api.post('/orders', order).then(function (res) {
		return res.data.id;
	});
}

function loadCart() {
	return api.get('/cart');
}
