// Package phonetics optionally looks up an IPA transcription when ECDICT lacks
// one. It is never required for offline translation, and never bulk-scrapes.
package phonetics
import("context";"encoding/json";"fmt";"io";"net/http";"net/url";"strings";"time")
type Client struct{HTTP *http.Client; BaseURL string}
type entry struct{Phonetic string `json:"phonetic"`; Phonetics []struct{Text string `json:"text"`} `json:"phonetics"`}
func(c Client) Get(ctx context.Context,word string)(string,error){
 if word==""||len(word)>72||strings.ContainsAny(word," /\\?&#\r\n") {return "",fmt.Errorf("无效单词")}
 base:=c.BaseURL;if base==""{base="https://api.dictionaryapi.dev/api/v2/entries/en"}
 req,e:=http.NewRequestWithContext(ctx,http.MethodGet,strings.TrimRight(base,"/")+"/"+url.PathEscape(word),nil);if e!=nil{return "",e}
 client:=c.HTTP;if client==nil{client=&http.Client{Timeout:3500*time.Millisecond}}
 resp,e:=client.Do(req);if e!=nil{return "",e};defer resp.Body.Close()
 if resp.StatusCode!=200 {return "",fmt.Errorf("IPA unavailable (%d)",resp.StatusCode)}
 var items []entry;if e=json.NewDecoder(io.LimitReader(resp.Body,32*1024)).Decode(&items);e!=nil{return "",e}
 for _,it:=range items {if s:=strings.TrimSpace(it.Phonetic);s!="" {return s,nil};for _,p:=range it.Phonetics{if s:=strings.TrimSpace(p.Text);s!="" {return s,nil}}}
 return "",fmt.Errorf("phonetic absent")
}
