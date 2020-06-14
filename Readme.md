# ln-fileserver

ln-fileserver is a work-in-progress file server which uses lnds signmessage api to authenticate users. Its fees can be customized to allow for economic usage.

The main goal is to store channel backups, so that users are able to restore their lightning node using only their seed.

## howto
- install with ```go get github.com/sputn1ck/ln-fileserver/...```
- run with ```ln-fileserver --lndconnect="LND_CONNECT_STRING" --data_dir="path/to/data/dir" --grpc_port=9090```
- cli can be run with ```lnfscli```
## lnfscli
```
NAME:
   lnfscli - cli for lightning network fileserver

USAGE:
   lnfscli [global options] command [command options] [arguments...]

COMMANDS:
   getinfo    returns information of ln-fileserver
   listfiles  returns all user owned files
   upload     uploads a file to the ln-fileserver
   download   downloads ln-fileserver
   uploadfee  estimates an uploadfee
   help, h    Shows a list of commands or help for one command

GLOBAL OPTIONS:
   --lndconnect value  lndconnect string
   --target value      target fileserver host (default: "localhost:9090")
   --help, -h          show help
```
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
