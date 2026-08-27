function recordMetric(name, value) {
	try {
		client.increment(name, value);
	} catch (e) {
		// best effort: metrics must never break the request
	}
}

function loadConfig() {
	return readConfigFile();
}
