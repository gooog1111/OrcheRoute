package online.gooog1111.orcheroute;

public final class FreeTurnUnderlayPolicyTest {
    public static void main(String[] args) {
        FreeTurnUnderlayPolicy policy = new FreeTurnUnderlayPolicy();
        policy.reset("wifi:1");
        require(!policy.observe("wifi:1", 0), "stable network must not reconnect");
        require(!policy.observe("", 100), "offline must preserve TUN and wait");
        require(policy.observe("mobile:2", 200), "WiFi loss then mobile must reconnect");
        require(!policy.observe("mobile:2", 300), "duplicate callback must not reconnect");
        require(!policy.observe("mobile:3", 400), "address burst must coalesce");
        require(policy.observe("mobile:3", 5200), "deferred change must not be lost");
        require(!policy.observe("", 6000), "offline is not a restart loop");
        require(policy.observe("mobile:3", 10300), "same network after loss still needs new allocations");
        policy.reset("wifi:4");
        require(!policy.observe("wifi:4", 11000), "new session must discard old recovery requests");
        require(policy.observe("wifi:5", 11100), "direct WiFi handover must reconnect");
        policy.reset("");
        require(policy.observe("mobile:6", 12000), "network arriving during startup must be detected");
        System.out.println("FreeTurnUnderlayPolicyTest passed");
    }

    private static void require(boolean value, String scenario) {
        if (!value) throw new AssertionError(scenario);
    }
}
