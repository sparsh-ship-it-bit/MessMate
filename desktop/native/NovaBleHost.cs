using System;
using System.Text;
using System.Text.Json;
using System.Threading.Tasks;
using Windows.Devices.Bluetooth.GenericAttributeProfile;
using Windows.Storage.Streams;

internal static class Program
{
    private static readonly Guid ServiceId = Guid.Parse("9b2d1000-4a7f-4c33-9b12-4d4553534d41");
    private static readonly Guid CommandId = Guid.Parse("9b2d1001-4a7f-4c33-9b12-4d4553534d41");

    private static async Task<int> Main()
    {
        try
        {
            var created = await GattServiceProvider.CreateAsync(ServiceId);
            if (created.Error != BluetoothError.Success || created.ServiceProvider is null)
            {
                Emit(new { type = "status", state = "unavailable", error = created.Error.ToString() });
                return 2;
            }

            var parameters = new GattLocalCharacteristicParameters
            {
                CharacteristicProperties = GattCharacteristicProperties.Write | GattCharacteristicProperties.WriteWithoutResponse,
                WriteProtectionLevel = GattProtectionLevel.Plain,
                UserDescription = "MessMate Nova Command"
            };
            var characteristicResult = await created.ServiceProvider.Service.CreateCharacteristicAsync(CommandId, parameters);
            if (characteristicResult.Error != BluetoothError.Success || characteristicResult.Characteristic is null)
            {
                Emit(new { type = "status", state = "unavailable", error = characteristicResult.Error.ToString() });
                return 3;
            }

            characteristicResult.Characteristic.WriteRequested += async (_, args) =>
            {
                var deferral = args.GetDeferral();
                try
                {
                    var request = await args.GetRequestAsync();
                    if (request?.Value is not null)
                    {
                        var reader = DataReader.FromBuffer(request.Value);
                        var bytes = new byte[reader.UnconsumedBufferLength];
                        reader.ReadBytes(bytes);
                        var action = Encoding.UTF8.GetString(bytes).Trim();
                        if (action is "dashboard" or "consumers" or "payments" or "attendance" or "workers" or "reports" or "refresh")
                            Emit(new { type = "command", action });
                        if (request.Option == GattWriteOption.WriteWithResponse)
                            request.Respond();
                    }
                }
                catch { }
                finally { deferral.Complete(); }
            };

            created.ServiceProvider.StartAdvertising(new GattServiceProviderAdvertisingParameters
            {
                IsConnectable = true,
                IsDiscoverable = true
            });
            Emit(new { type = "status", state = "advertising" });

            await Task.Delay(Timeout.Infinite);
            return 0;
        }
        catch (Exception ex)
        {
            Emit(new { type = "status", state = "unavailable", error = ex.Message });
            return 1;
        }
    }

    private static void Emit(object value)
    {
        Console.WriteLine(JsonSerializer.Serialize(value));
        Console.Out.Flush();
    }
}
