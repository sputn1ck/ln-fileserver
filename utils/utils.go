package utils

import "github.com/sputn1ck/ln-fileserver/api"

func GetUploadChunkFee(chunksize int, expiry int64, fees *api.FeeReport) int64 {
	chunkKB := int64(float64(chunksize) / float64(1024))
	hoursStored := expiry / 3600
	msatCost := fees.MsatPerHourPerKB * hoursStored * (chunkKB)
	return msatCost
}

func GetDownloadChunkFee(chunksize int, fees *api.FeeReport) int64 {
	chunkKB := int64(float64(chunksize) / float64(1024))
	msatCost := fees.MsatPerHourPerKB * (chunkKB )
	return msatCost
}

func GetTotalUploadFee(filesize int64, expiry int64, fees *api.FeeReport) int64 {
	chunkKB := int64(float64(filesize) / float64(1024))
	hoursStored := int64(expiry) / 3600
	msatCost := fees.MsatPerHourPerKB * hoursStored * (chunkKB)
	return msatCost
}

func GetTotalDownloadFee(filesize int64, fees *api.FeeReport) int64 {
	chunkKB := int64(float64(filesize) / float64(1024))
	msatCost := fees.MsatPerHourPerKB * (chunkKB)
	return msatCost
}