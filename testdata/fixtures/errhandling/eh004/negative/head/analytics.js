function track(event) {
	beacon.send(event).catch(() => {});
}

function loadDashboard() {
	return api.get('/dashboard').catch((err) => {
		console.error('dashboard load failed', err);
		return null;
	});
}
