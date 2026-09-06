package online.gooog1111.orcheroute;

import android.content.BroadcastReceiver;
import android.content.Context;
import android.content.Intent;
import android.net.VpnService;
import android.util.Log;

/** Restores only the VPN state explicitly left enabled by the user. */
public final class BootReceiver extends BroadcastReceiver {
    @Override
    public void onReceive(Context context, Intent intent) {
        if (intent == null || !Intent.ACTION_BOOT_COMPLETED.equals(intent.getAction())) return;
        try {
            boolean desired = new MobileRepository(context.getApplicationContext()).serviceDesired();
            boolean permitted = VpnService.prepare(context) == null;
            if (BootPolicy.shouldRestore(desired, permitted)) {
                OrcheRouteVpnService.start(context.getApplicationContext());
            } else if (desired) {
                Log.w("OrcheRouteBoot", "VPN remained enabled but Android VPN permission must be granted again");
            }
        } catch (Throwable error) {
            Log.e("OrcheRouteBoot", "Unable to restore persisted VPN state", error);
        }
    }
}
