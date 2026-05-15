package runtime

import "context"

func TakeScreenshot(ctx context.Context) ([]byte, error) {
	appFrontend := getFrontend(ctx)
	return appFrontend.TakeScreenshot()
}
