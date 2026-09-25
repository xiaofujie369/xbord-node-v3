<?php

namespace Tests\Feature\Server;

use App\Http\Requests\Admin\ServerSave;
use App\Http\Controllers\V1\Client\ClientController;
use Illuminate\Http\Request;
use App\Models\Server;
use App\Models\User;
use App\Services\ServerService;
use Illuminate\Contracts\Console\Kernel;
use Illuminate\Support\Facades\Validator;
use Tests\TestCase;

class AnyTLSRealityTest extends TestCase
{
    public function createApplication()
    {
        $app = require __DIR__ . '/../../../bootstrap/app.php';
        $app->afterBootstrapping(\Illuminate\Foundation\Bootstrap\LoadConfiguration::class, function ($app) {
            $app['config']->set([
                'app.key' => 'base64:' . base64_encode(str_repeat('a', 32)),
                'database.default' => 'sqlite',
                'database.connections.sqlite.database' => ':memory:',
                'cache.default' => 'array',
                'cache.stores.redis' => ['driver' => 'array'],
                'session.driver' => 'array',
                'queue.default' => 'sync',
            ]);
        });
        $app->make(Kernel::class)->bootstrap();
        return $app;
    }

    private function reality(): array
    {
        $private = random_bytes(32);
        $encode = fn ($value) => rtrim(strtr(base64_encode($value), '+/', '-_'), '=');
        return [
            'server_name' => 'example.com', 'dest' => 'example.com:443',
            'private_key' => $encode($private),
            'public_key' => $encode(sodium_crypto_scalarmult_base($private)),
            'short_id' => 'aabb0123',
        ];
    }

    private function node(array $settings): Server
    {
        return new Server([
            'type' => 'anytls', 'name' => 'test', 'host' => '127.0.0.1',
            'port' => '39443', 'server_port' => 39443, 'rate' => 1,
            'group_ids' => ['1'], 'show' => true, 'protocol_settings' => $settings,
        ]);
    }

    public function test_legacy_storage_and_requests_preserve_standard_tls(): void
    {
        $old = ['tls' => ['server_name' => 'legacy.example', 'allow_insecure' => true], 'padding_scheme' => ['stop=8']];
        $node = $this->node($old);
        $this->assertSame(1, $node->protocol_settings['tls']);
        $node->setRawAttributes(['type' => 'anytls', 'protocol_settings' => json_encode($old), 'server_port' => 39443]);
        $config = ServerService::buildNodeConfig($node);
        $this->assertSame(1, $config['tls']);
        $this->assertSame('legacy.example', $config['tls_settings']['server_name']);
        $this->assertTrue($config['tls_settings']['allow_insecure']);
        $this->assertSame(['stop=8'], $config['padding_scheme']);
    }

    public function test_modes_round_trip_and_reality_never_requests_certificates(): void
    {
        $reality = $this->reality();
        foreach ([0, 1, 2] as $mode) {
            $node = $this->node(['tls' => $mode, 'tls_settings' => ['server_name' => 'tls.example'], 'reality_settings' => $reality]);
            $node->cert_config = ['mode' => 'http', 'cert_domain' => 'tls.example'];
            $config = ServerService::buildNodeConfig($node);
            $this->assertSame($mode, $config['tls']);
            $this->assertSame($mode === 1, isset($config['cert_config']));
            $this->assertSame($mode === 2 ? 'example.com' : 'tls.example', $config['tls_settings']['server_name']);
            if ($mode === 2) {
                foreach ($reality as $key => $value) {
                    $this->assertSame($value, $config['tls_settings'][$key]);
                }
            }
        }
    }

    public function test_shared_reality_protocols_keep_independent_node_keys(): void
    {
        $configs = [];
        foreach (['vless', 'trojan', 'anytls'] as $type) {
            $reality = $this->reality();
            $node = new Server(['type' => $type, 'server_port' => 39443,
                'protocol_settings' => ['tls' => 2, 'reality_settings' => $reality]]);
            $configs[] = ServerService::buildNodeConfig($node);
            $this->assertSame($reality['private_key'], end($configs)['tls_settings']['private_key']);
            $this->assertSame(2, end($configs)['tls']);
        }
        $this->assertCount(3, array_unique(array_column(array_column($configs, 'tls_settings'), 'private_key')));
    }

    public function test_admin_validation_rejects_bad_reality_and_accepts_valid_pair(): void
    {
        $valid = ['tls' => 2, 'reality_settings' => $this->reality()];
        $validate = function ($settings) {
            $request = ServerSave::create('/', 'POST', [
                'type' => 'anytls', 'name' => 'test', 'host' => '127.0.0.1',
                'port' => 39443, 'server_port' => 39443, 'rate' => 1,
                'protocol_settings' => $settings,
            ]);
            $request->setContainer($this->app);
            (new \ReflectionMethod($request, 'prepareForValidation'))->invoke($request);
            $validator = Validator::make($request->all(), $request->rules());
            $request->withValidator($validator);
            return $validator;
        };
        $this->assertTrue($validate($valid)->passes());
        $this->assertTrue($validate(['tls' => ['server_name' => 'legacy.example']])->passes());
        foreach (['dest' => 'example.com:70000', 'short_id' => 'abc', 'public_key' => str_repeat('A', 43), 'server_name' => 'bad name'] as $field => $bad) {
            $settings = $valid;
            $settings['reality_settings'][$field] = $bad;
            $this->assertTrue($validate($settings)->fails(), $field);
        }
        $this->assertTrue($validate(['tls' => 2])->fails());
        $this->assertTrue($validate(['tls' => 3])->fails());
    }

    public function test_persistence_and_client_boundary_remove_private_key(): void
    {
        $this->artisan('migrate', ['--force' => true])->assertExitCode(0);
        $reality = $this->reality();
        $node = $this->node(['tls' => 2, 'reality_settings' => $reality]);
        $node->save();
        $this->assertSame($reality['private_key'], $node->fresh()->protocol_settings['reality_settings']['private_key']);
        $user = new User(['group_id' => 1, 'uuid' => 'test-uuid']);
        $servers = ServerService::getAvailableServers($user);
        $this->assertCount(1, $servers);
        $this->assertArrayNotHasKey('private_key', $servers[0]['protocol_settings']['reality_settings']);
        $this->assertSame($reality['public_key'], $servers[0]['protocol_settings']['reality_settings']['public_key']);
        $this->assertSame('test-uuid', $servers[0]['password']);
    }

    public function test_authenticated_node_api_delivers_reality_and_rejects_wrong_token(): void
    {
        $this->artisan('migrate', ['--force' => true])->assertExitCode(0);
        admin_setting(['server_token' => 'local-test-token']);
        $reality = $this->reality();
        $node = $this->node(['tls' => 2, 'reality_settings' => $reality, 'padding_scheme' => ['stop=8']]);
        $node->save();
        $url = '/api/v2/server/config?node_id=' . $node->id;
        $this->getJson($url . '&token=wrong')->assertStatus(422);
        $response = $this->getJson($url . '&token=local-test-token')->assertOk()
            ->assertJsonPath('tls', 2)
            ->assertJsonPath('tls_settings.dest', 'example.com:443')
            ->assertJsonPath('tls_settings.private_key', $reality['private_key'])
            ->assertJsonPath('padding_scheme.0', 'stop=8');
        $etag = $response->headers->get('ETag');
        $this->getJson($url . '&token=local-test-token', ['If-None-Match' => $etag])->assertStatus(304);
        $settings = $node->protocol_settings;
        $settings['reality_settings']['short_id'] = 'ffee';
        $node->protocol_settings = $settings;
        $node->save();
        $this->getJson($url . '&token=local-test-token', ['If-None-Match' => $etag])
            ->assertOk()->assertJsonPath('tls_settings.short_id', 'ffee');
    }

    public function test_admin_save_validates_before_persisting_and_retains_legacy_fields(): void
    {
        $this->artisan('migrate', ['--force' => true])->assertExitCode(0);
        $route = collect($this->app['router']->getRoutes())->first(fn ($route) =>
            $route->getActionName() === 'App\\Http\\Controllers\\V2\\Admin\\Server\\ManageController@save');
        $this->assertNotNull($route);
        $url = '/' . $route->uri();
        $data = $this->node(['tls' => 2, 'reality_settings' => $this->reality()])->toArray();
        $this->postJson($url, $data)->assertForbidden();
        $admin = new User(['is_admin' => true]);
        $admin->id = 1;
        \Laravel\Sanctum\Sanctum::actingAs($admin);
        $this->postJson($url, $data)->assertOk();
        $node = Server::firstOrFail();
        $this->assertSame(2, $node->protocol_settings['tls']);
        $audit = \App\Models\AdminAuditLog::latest('id')->firstOrFail();
        $this->assertStringNotContainsString($data['protocol_settings']['reality_settings']['private_key'], json_encode($audit->request_data));
        $this->assertStringContainsString($data['protocol_settings']['reality_settings']['public_key'], json_encode($audit->request_data));
        $bad = $data;
        $bad['id'] = $node->id;
        $bad['protocol_settings']['reality_settings']['short_id'] = 'xyz';
        $this->postJson($url, $bad)->assertStatus(422);
        $this->assertSame($data['protocol_settings']['reality_settings'], $node->fresh()->protocol_settings['reality_settings']);
        $data['name'] = 'legacy';
        $data['protocol_settings'] = ['tls' => ['server_name' => 'legacy.example']];
        $this->postJson($url, $data)->assertOk();
        $legacy = Server::where('name', 'legacy')->firstOrFail();
        $this->assertSame(1, $legacy->protocol_settings['tls']);
        $this->assertSame('legacy.example', $legacy->protocol_settings['tls_settings']['server_name']);
    }

    public function test_standard_subscription_preserves_sni_and_uuid_and_excludes_unsupported_modes(): void
    {
        $this->artisan('migrate', ['--force' => true])->assertExitCode(0);
        $user = new User(['uuid' => 'fixture-uuid', 'u' => 0, 'd' => 0, 'transfer_enable' => 100000]);
        $request = Request::create('/subscribe?flag=sing-box');
        $request->headers->set('User-Agent', 'sing-box/1.13.0');
        $servers = [];
        foreach ([0, 1, 2] as $mode) {
            $node = $this->node(['tls' => $mode, 'tls_settings' => ['server_name' => 'tls.example'], 'reality_settings' => $this->reality()]);
            $node->name = 'mode-' . $mode;
            $servers[] = $node->toArray();
        }
        $response = (new ClientController())->doSubscribe($request, $user, $servers);
        $json = json_decode($response->getContent(), true, flags: JSON_THROW_ON_ERROR);
        $outbounds = array_values(array_filter($json['outbounds'], fn ($outbound) => ($outbound['type'] ?? '') === 'anytls'));
        $this->assertNotEmpty($outbounds);
        foreach ($outbounds as $outbound) {
            $this->assertSame('tls.example', $outbound['tls']['server_name']);
            $this->assertSame('fixture-uuid', $outbound['password']);
        }
        $this->assertStringNotContainsString('mode-0', $response->getContent());
        $this->assertStringNotContainsString('mode-2', $response->getContent());
        $this->assertStringNotContainsString('private_key', $response->getContent());
    }
}
