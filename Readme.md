# ln-fileserver

ln-fileserver is a work-in-progress file server which uses lnds signmessage api to authenticate users. Its upload and download fees can be heavily customized to allow for economic usage.

The main goal is to store channel backups, so that users are able to restore their lightning node using only their seed.

## howto
- install with ```go get github.com/sputn1ck/ln-fileserver/...```
- run with ```ln-fileserver --lndconnect="LND_CONNECT_STRING" --data_dir="path/to/data/dir" --grpc_port=9090```
- cli can be run with ```lnfscli```
- it always need the lndconnect string ```lnfscli --lndconnect="LND_CONNECT_STRING getinfo``` (you can create an alias)
## fees

current fee options are:

- msat_base_cost -> charged at the beginning of an upload process
- msat_per_hour_per_k_b -> msats per hour and kilobyte stored
- msat_per_downloaded_k_b -> msats per kilobyte downloaded

## upload
```
create File slot ->
<- Creation Invoice (base Cost)
Pay ->
for uploading {
    Chunk ->
    <- Chunk Invoice  
    Pay ->
}
Finished ->
<- FileSlot Info
```
## download
```
download request->
<- FileSlot Info
for downloading {
    <- Chunk Invoice
    -> Pay
    <- Chunk
}
<- Finished
```
