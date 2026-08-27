async function loadPreferences(userId) {
	const res = await fetch(`/api/users/${userId}/preferences`);
	return res.json();
}

async function sendNotification(userId, message) {
	try {
		await fetch('/api/notify', {
			method: 'POST',
			body: JSON.stringify({ userId, message }),
		});
	} catch (err) {
	}
}
