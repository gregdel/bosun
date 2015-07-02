interface IActionScope extends IExprScope {
	type: string;
	user: string;
	message: string;
	notify: boolean;
	keys: string[];
	submit: () => void;
}

bosunControllers.controller('ActionCtrl', ['$scope', '$http', '$location', '$route', function($scope: IActionScope, $http: ng.IHttpService, $location: ng.ILocationService, $route: ng.route.IRouteService) {
	var search = $location.search();
	$scope.user = readCookie("action-user");
	$scope.type = search.type;
	$scope.notify = true;
	if (search.key) {
		var keys = search.key;
		if (!angular.isArray(search.key)) {
			keys = [search.key];
		}
		$location.search('key', null);
		$scope.setKey('action-keys', keys);
	} else {
		$scope.keys = $scope.getKey('action-keys');
	}
	$scope.submit = () => {
		var data = {
			Type: $scope.type,
			User: $scope.user,
			Message: $scope.message,
			Keys: $scope.keys,
			Notify: $scope.notify,
		};
		createCookie("action-user", $scope.user, 1000);
		$http.post('/api/action', data)
			.success((data) => {
				$location.url('/');
			})
			.error((error) => {
				alert(error);
			});
	};
}]);
