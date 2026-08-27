function recordMetric(name, value) {
	try {
		client.increment(name, value);
	} catch (e) {
		// best effort: metrics must never break the request
	}
}

function loadConfig() {
	try {
		return readConfigFile();
	} catch (err) {
		console.error('failed to load config', err);
		return defaultConfig();
	}
}
