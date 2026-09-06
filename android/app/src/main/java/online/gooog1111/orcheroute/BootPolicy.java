package online.gooog1111.orcheroute;

/** Pure decision kept separate so reboot behaviour can be tested without Android. */
final class BootPolicy {
    private BootPolicy() { }

    static boolean shouldRestore(boolean desiredEnabled, boolean vpnPermissionGranted) {
        return desiredEnabled && vpnPermissionGranted;
    }
}
