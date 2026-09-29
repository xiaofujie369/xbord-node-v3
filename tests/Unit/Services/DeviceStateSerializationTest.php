<?php

namespace Tests\Unit\Services;

use App\Services\DeviceStateService;
use Illuminate\Support\Facades\Redis;
use Mockery;
use PHPUnit\Framework\TestCase;

class DeviceStateSerializationTest extends TestCase
{
    public function test_duplicate_ips_remain_a_json_list_and_expired_devices_are_excluded(): void
    {
        $now = time();
        $redis = Mockery::mock();
        $redis->shouldReceive('hgetall')->once()->with('user_devices:7')->andReturn([
            '1:192.0.2.1' => $now,
            '2:192.0.2.1' => $now,
            '3:2001:db8::1' => $now,
            '4:192.0.2.9' => $now - 3600,
        ]);
        Redis::swap($redis);
        try {
            $devices = (new DeviceStateService())->getUsersDevices([7]);
            $this->assertSame(['192.0.2.1', '2001:db8::1'], $devices[7]);
            $wire = json_decode(json_encode(['users' => $devices]));
            $this->assertIsArray($wire->users->{'7'});
        } finally {
            Redis::clearResolvedInstance('redis');
            Mockery::close();
        }
    }
}
