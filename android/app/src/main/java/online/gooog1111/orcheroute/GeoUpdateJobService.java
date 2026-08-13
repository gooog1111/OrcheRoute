package online.gooog1111.orcheroute;

import android.app.job.JobParameters;
import android.app.job.JobService;

import org.json.JSONObject;

import java.io.File;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;

import mobilecore.Mobilecore;

public final class GeoUpdateJobService extends JobService {
    private final ExecutorService worker = Executors.newSingleThreadExecutor();

    @Override
    public boolean onStartJob(JobParameters params) {
        worker.execute(() -> {
            boolean retry = true;
            try {
                File home = new File(getFilesDir(), "mihomo");
                MobileRepository repository = new MobileRepository(this);
                JSONObject settings = repository.componentSettings();
                JSONObject result = new JSONObject(Mobilecore.updateGeoFromSource(home.getAbsolutePath(),
                        settings.optString("geo_source", "metacubex"), settings.optString("geoip_url", ""),
                        settings.optString("geosite_url", "")));
                retry = !result.optBoolean("ok", false);
                if (!retry) repository.saveInstalledGeoSource(result.getJSONObject("result").getJSONObject("source"));
            } catch (Throwable ignored) {
                retry = true;
            }
            jobFinished(params, retry);
        });
        return true;
    }

    @Override
    public boolean onStopJob(JobParameters params) {
        return true;
    }

    @Override
    public void onDestroy() {
        worker.shutdownNow();
        super.onDestroy();
    }
}
