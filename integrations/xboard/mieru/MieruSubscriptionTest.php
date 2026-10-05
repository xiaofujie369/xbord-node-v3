<?php

namespace Tests\Unit\Protocols;

use App\Protocols\ClashMeta;
use PHPUnit\Framework\TestCase;

class MieruSubscriptionTest extends TestCase
{
    public function test_native_pattern_survives_subscription_and_port_range_is_exclusive(): void
    {
        $server = ['name' => 'lab', 'host' => '192.0.2.1', 'port' => 20001,
            'protocol_settings' => ['transport' => 'TCP', 'traffic_pattern' => 'CI3R1AkQABoECAEQACICCAA=']];
        $proxy = ClashMeta::buildMieru('fixture-uuid', $server);
        $this->assertSame($server['protocol_settings']['traffic_pattern'], $proxy['traffic-pattern']);
        $this->assertSame('fixture-uuid', $proxy['username']);
        $this->assertSame('fixture-uuid', $proxy['password']);
        $this->assertSame(20001, $proxy['port']);
        $this->assertArrayNotHasKey('port-range', $proxy);
        $server['ports'] = '20001-20002';
        $proxy = ClashMeta::buildMieru('fixture-uuid', $server);
        $this->assertSame('20001-20002', $proxy['port-range']);
        $this->assertArrayNotHasKey('port', $proxy);
        unset($server['protocol_settings']['traffic_pattern']);
        $this->assertArrayNotHasKey('traffic-pattern', ClashMeta::buildMieru('fixture-uuid', $server));
    }
}
