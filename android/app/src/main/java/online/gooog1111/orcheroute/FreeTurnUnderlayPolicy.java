package online.gooog1111.orcheroute;

/** Android underlay lifecycle only; never changes the user's selected server. */
final class FreeTurnUnderlayPolicy {
    private String network = "";
    private boolean pending;
    private long lastReconnect;

    synchronized void reset(String initialNetwork) {
        network = initialNetwork;
        pending = false;
        lastReconnect = -5000;
    }

    synchronized boolean observe(String currentNetwork, long elapsedMs) {
        if (!network.equals(currentNetwork)) {
            network = currentNetwork;
            pending = true;
        }
        // Preserve TUN while offline. Coalesce address/network callbacks, but
        // retain a deferred change so the next poll can apply the final network.
        if (network.isEmpty() || !pending || elapsedMs - lastReconnect < 5000) return false;
        pending = false;
        lastReconnect = elapsedMs;
        return true;
    }
}
