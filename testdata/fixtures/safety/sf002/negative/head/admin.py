@login_required
def admin_panel(request):
    return render(request, "admin.html")
