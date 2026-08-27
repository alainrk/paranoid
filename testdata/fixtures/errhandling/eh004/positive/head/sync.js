function syncData() {
	fetchData().then(handle).catch(() => {});
}
