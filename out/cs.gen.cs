using System.Collections.Generic;
using System.Text.Json.Serialization;

public class AllConfig
{
    [JsonPropertyName("items")]
    public List<Item> Items { get; set; }

    [JsonPropertyName("quests")]
    public List<Quest> Quests { get; set; }

}

public class Item
{
    [JsonPropertyName("cid")]
    public int Cid { get; set; }

    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("data")]
    public string Data { get; set; }

    [JsonPropertyName("dt")]
    public List<int> Dt { get; set; }

    [JsonPropertyName("dtArr")]
    public List<List<int>> DtArr { get; set; }

}

public class Quest
{
    [JsonPropertyName("cid")]
    public int Cid { get; set; }

    [JsonPropertyName("count")]
    public int Count { get; set; }

    [JsonPropertyName("data")]
    public string Data { get; set; }

    [JsonPropertyName("dt")]
    public List<int> Dt { get; set; }

    [JsonPropertyName("dtArr")]
    public List<List<int>> DtArr { get; set; }

}
