package online.gooog1111.orcheroute;

import android.app.job.JobInfo;
import android.app.job.JobScheduler;
import android.content.ComponentName;
import android.content.Context;

import java.util.concurrent.TimeUnit;

final class GeoUpdateScheduler {
    static final int JOB_ID = 1043;

    static void apply(Context context, boolean enabled, int intervalHours) {
        JobScheduler scheduler = context.getSystemService(JobScheduler.class);
        if (scheduler == null) return;
        if (!enabled) {
            scheduler.cancel(JOB_ID);
            return;
        }
        long interval = TimeUnit.HOURS.toMillis(Math.max(6, Math.min(168, intervalHours)));
        JobInfo job = new JobInfo.Builder(JOB_ID, new ComponentName(context, GeoUpdateJobService.class))
                .setRequiredNetworkType(JobInfo.NETWORK_TYPE_ANY)
                .setPeriodic(interval)
                .setPersisted(true)
                .build();
        scheduler.schedule(job);
    }

    private GeoUpdateScheduler() { }
}
