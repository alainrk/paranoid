function track(event) {
	beacon.send(event).catch(() => {});
}

function loadDashboard() {
	return api.get('/dashboard');
}
